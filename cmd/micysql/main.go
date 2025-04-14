package main

import (
	"database/sql"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/gofiber/fiber/v2"
	"github.com/jordanhenderson/icebreak"
)

const (
	mysqlSock = "/tmp/mysql.sock"
	mysqlData = "/tmp/micydb"
)

var (
	db     *sql.DB
	dbOnce sync.Once

	dsn = "root@unix(" + mysqlSock + ")/mysql"
)

func launchMariaDB() {
	log.Println("🧊 MicySQL background launch initiated...")

	if _, err := os.Stat(mysqlData); os.IsNotExist(err) {
		os.Mkdir(mysqlData, 0755)
		log.Println("📂 Copying prebuilt data dir from /var/task/data to /tmp/micydb...")
		err := exec.Command("cp", "-a", "/var/task/data/.", mysqlData).Run()
		if err != nil {
			log.Fatalf("❌ Failed to copy data dir: %v", err)
		}
	}

	os.Setenv("LD_LIBRARY_PATH", "/var/task/lib")

	args := []string{
		"--user=root",
		"--datadir=" + mysqlData,
		"--socket=" + mysqlSock,
		"--skip-networking",
		"--pid-file=/tmp/mysqld.pid",
		"--innodb-use-native-aio=0",
		"--innodb-flush-method=fsync",
		"--innodb-data-file-path=ibdata1:2M:autoextend:max:256M",
		"--innodb-temp-data-file-path=ibtmp1:2M:autoextend:max:64M",
		"--innodb-buffer-pool-size=64M",
		"--innodb-file-per-table=1",
	}

	log.Printf("📦 Launching mariadbd with args:\n  %s", strings.Join(args, " "))

	cmd := exec.Command("/var/task/bin/mariadbd", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		log.Fatalf("❌ Failed to start mariadbd: %v", err)
	}
}

func waitForMariaDB(timeout time.Duration) {
	log.Printf("⏳ Waiting for MariaDB socket at %s (max %s)...", mysqlSock, timeout)
	deadline := time.Now().Add(timeout)
	for {
		if _, err := os.Stat(mysqlSock); err == nil {
			log.Println("✅ Socket ready.")
			return
		}
		if time.Now().After(deadline) {
			log.Fatalf("❌ Timeout waiting for MariaDB socket")
		}
		time.Sleep(300 * time.Millisecond)
	}
}

func ensureMariaDBReady() {
	// Only block here on first request
	waitForMariaDB(300 * time.Second)

	conn, err := sql.Open("mysql", dsn)
	if err != nil {
		log.Printf("❌ Failed to connect to DB: %v", err)
	}
	if err := conn.Ping(); err != nil {
		log.Printf("❌ Ping failed: %v", err)
	}
	db = conn
	log.Println("✅ Connected to MariaDB.")

}

func main() {
	icebreak.InitLambda()
	launchMariaDB() // Non-blocking mariadbd startup during init
	waitForMariaDB(9 * time.Second)

	conn, err := sql.Open("mysql", dsn)
	if err != nil {
		log.Printf("❌ Failed to connect to DB: %v", err)
	}
	if err := conn.Ping(); err != nil {
		log.Printf("❌ Ping failed: %v", err)
	}
	db = conn

	app := fiber.New()

	app.Post("/execute", func(c *fiber.Ctx) error {
		ensureMariaDBReady()

		var req struct {
			SQL string `json:"sql"`
		}
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "Invalid JSON"})
		}

		if strings.TrimSpace(req.SQL) == "" {
			return c.Status(400).JSON(fiber.Map{"error": "Empty SQL"})
		}

		sqlText := strings.TrimSpace(req.SQL)
		if strings.HasPrefix(strings.ToUpper(sqlText), "SELECT") {
			rows, err := db.Query(req.SQL)
			if err != nil {
				return c.Status(500).JSON(fiber.Map{"error": err.Error()})
			}
			defer rows.Close()

			cols, _ := rows.Columns()
			colTypes, _ := rows.ColumnTypes()
			results := []map[string]interface{}{}

			for rows.Next() {
				values := make([]interface{}, len(cols))
				raw := make([]sql.RawBytes, len(cols))

				for i := range raw {
					values[i] = &raw[i]
				}

				if err := rows.Scan(values...); err != nil {
					return c.Status(500).JSON(fiber.Map{"error": "Row scan failed", "details": err.Error()})
				}

				rowMap := map[string]interface{}{}
				for i, col := range cols {
					rawVal := raw[i]
					if rawVal == nil {
						rowMap[col] = nil
						continue
					}

					// Convert by database type
					switch colTypes[i].DatabaseTypeName() {
					case "INT", "TINYINT", "BIGINT", "SMALLINT", "MEDIUMINT":
						if val, err := strconv.Atoi(string(rawVal)); err == nil {
							rowMap[col] = val
						} else {
							rowMap[col] = string(rawVal)
						}
					case "FLOAT", "DOUBLE", "DECIMAL":
						if val, err := strconv.ParseFloat(string(rawVal), 64); err == nil {
							rowMap[col] = val
						} else {
							rowMap[col] = string(rawVal)
						}
					case "VARCHAR", "TEXT", "CHAR", "ENUM":
						rowMap[col] = string(rawVal)
					case "DATE", "DATETIME", "TIMESTAMP":
						rowMap[col] = string(rawVal) // You can parse into time.Time if desired
					default:
						// Fallback: just send as string
						rowMap[col] = string(rawVal)
					}
				}
				results = append(results, rowMap)
			}

			return c.JSON(fiber.Map{
				"columns": cols,
				"rows":    results,
			})
		} else {
			// INSERT/UPDATE/DELETE/DDL
			result, err := db.Exec(sqlText)
			if err != nil {
				return c.Status(500).JSON(fiber.Map{"error": err.Error()})
			}
			rowsAffected, _ := result.RowsAffected()
			return c.JSON(fiber.Map{
				"result":        "ok",
				"rows_affected": rowsAffected,
			})
		}

	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("🌐 Fiber HTTP server listening on :%s", port)
	log.Fatal(app.Listen(":" + port))
}
