// main.go
package main

import (
	"database/sql"
	"log"
	"os"
	"os/exec"
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
	db        *sql.DB
	dbOnce    sync.Once
	dbReady   = false
	dbReadyMu sync.Mutex
)

func launchMariaDB() {
	log.Println("🧊 MicySQL cold boot initiated...")

	// Ensure data dir exists
	initDataDir := ""
	if _, err := os.Stat(mysqlData); os.IsNotExist(err) {
		os.Mkdir(mysqlData, 0755)
		initDataDir = "--initialize-insecure"
	}

	os.Setenv("LD_LIBRARY_PATH", "/var/task/lib")

	log.Println("🚀 Starting mariadbd daemon...")
	cmd := exec.Command(
		"/var/task/bin/mariadbd",
		"--datadir="+mysqlData,
		"--socket="+mysqlSock,
		"--no-defaults",
		"--skip-networking",
		initDataDir,
		"--pid-file=/tmp/mysqld.pid",
		"--log-error=/tmp/mysqld.err",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		log.Fatalf("❌ Failed to start mariadbd: %v", err)
	}

	waitForSocket(mysqlSock, 30*time.Second)

	dsn := "root@unix(" + mysqlSock + ")/mysql"
	dbLocal, err := sql.Open("mysql", dsn)
	if err != nil {
		log.Fatalf("❌ Failed to open DB: %v", err)
	}

	for i := 0; i < 20; i++ {
		if err := dbLocal.Ping(); err == nil {
			db = dbLocal
			log.Println("✅ MariaDB responds to queries")
			dbReadyMu.Lock()
			dbReady = true
			dbReadyMu.Unlock()
			return
		}
		time.Sleep(500 * time.Millisecond)
	}

	log.Fatal("❌ MariaDB ping timed out")
}

func waitForSocket(path string, timeout time.Duration) {
	log.Printf("⏳ Waiting for socket at %s (max %s)...", path, timeout)
	deadline := time.Now().Add(timeout)
	for {
		if _, err := os.Stat(path); err == nil {
			log.Println("✅ Socket ready:", path)
			return
		}
		if time.Now().After(deadline) {
			log.Fatalf("❌ Timeout waiting for MariaDB socket")
		}
		time.Sleep(300 * time.Millisecond)
	}
}

func ensureMariaDBReady() {
	dbOnce.Do(func() {
		launchMariaDB()
	})
}

func main() {
	icebreak.InitLambda()
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

		rows, err := db.Query(req.SQL)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		defer rows.Close()

		cols, _ := rows.Columns()
		results := []map[string]interface{}{}

		for rows.Next() {
			values := make([]interface{}, len(cols))
			ptrs := make([]interface{}, len(cols))
			for i := range values {
				ptrs[i] = &values[i]
			}
			rows.Scan(ptrs...)
			rowMap := map[string]interface{}{}
			for i, col := range cols {
				rowMap[col] = values[i]
			}
			results = append(results, rowMap)
		}

		return c.JSON(fiber.Map{
			"columns": cols,
			"rows":    results,
		})
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("🌐 Fiber HTTP server listening on :%s", port)
	log.Fatal(app.Listen(":" + port))
}
