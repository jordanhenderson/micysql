// File: cmd/micysql-proxy/main.go
package main

import (
	"bytes"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"github.com/joho/godotenv"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"time"
)

const (
	listenAddr = ":3306"
)

type SQLRequest struct {
	SQL string `json:"sql"`
}

type SQLResponse struct {
	Columns []string        `json:"columns"`
	Rows    [][]interface{} `json:"rows"`
	Error   string          `json:"error,omitempty"`
}

var lambdaURL = ""

func main() {
	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		log.Fatalf("Failed to listen on %s: %v", listenAddr, err)
	}
	log.Printf("Listening on %s", listenAddr)

	_ = godotenv.Load("/tmp/.env")
	f, _ := os.ReadFile("/tmp/.env")
	log.Printf("Loaded .env: %s", f)

	lambdaURL = os.Getenv("MICYSQL_URL")
	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Println("Accept error:", err)
			continue
		}
		go handleConnection(conn)
	}
}

func handleConnection(conn net.Conn) {
	defer conn.Close()
	seq := byte(0)

	handshake := createHandshakePacket()
	conn.Write(writePacket(handshake, seq))
	seq++

	// Read client handshake
	head := make([]byte, 4)
	if _, err := io.ReadFull(conn, head); err != nil {
		log.Println("Handshake read error:", err)
		return
	}
	length := int(head[0]) | int(head[1])<<8 | int(head[2])<<16
	payload := make([]byte, length)
	if _, err := io.ReadFull(conn, payload); err != nil {
		log.Println("Payload read error:", err)
		return
	}

	ok := []byte{0x00, 0x00, 0x00, 0x02, 0x00, 0x00, 0x00}
	conn.Write(writePacket(ok, seq))
	seq++

	for {
		head := make([]byte, 4)
		if _, err := io.ReadFull(conn, head); err != nil {
			return
		}
		length := int(head[0]) | int(head[1])<<8 | int(head[2])<<16
		payload := make([]byte, length)
		if _, err := io.ReadFull(conn, payload); err != nil {
			return
		}
		if payload[0] != 0x03 {
			continue // Not a COM_QUERY
		}
		query := string(payload[1:])
		log.Println("Forwarding query:", query)
		resp, err := forwardQuery(query)
		if err != nil {
			conn.Write(writePacket(buildErrorPacket(err.Error()), seq))
			seq++
			continue
		}
		// Return resultset header
		conn.Write(writePacket([]byte{byte(len(resp.Columns))}, seq))
		seq++
		for _, col := range resp.Columns {
			colDef := buildColumnDefinition(col)
			conn.Write(writePacket(colDef, seq))
			seq++
		}
		conn.Write(writePacket([]byte{0xfe, 0x00, 0x00, 0x02, 0x00, 0x00}, seq)) // EOF
		seq++
		for _, row := range resp.Rows {
			rowPayload := buildRowPacket(row)
			conn.Write(writePacket(rowPayload, seq))
			seq++
		}
		conn.Write(writePacket([]byte{0xfe, 0x00, 0x00, 0x02, 0x00, 0x00}, seq)) // EOF
		seq++
	}
}

func writePacket(payload []byte, seq byte) []byte {
	head := make([]byte, 4)
	binary.LittleEndian.PutUint32(head, uint32(len(payload)))
	head[3] = seq
	return append(head[:3], append([]byte{seq}, payload...)...)
}

func buildOKPacket() []byte {
	return []byte{0x00, 0x00, 0x00, 0x02, 0x00, 0x00, 0x00} // Basic OK packet
}

func buildErrorPacket(msg string) []byte {
	buf := []byte{0xff, 0x48, 0x04, '#'} // Error header
	buf = append(buf, []byte("HY000")...)
	buf = append(buf, []byte(msg)...)
	return buf
}

func buildColumnDefinition(name string) []byte {
	// Minimal column definition (mocked fields)
	buf := []byte{0x03, 'd', 'b', '1'}               // catalog
	buf = append(buf, 0x05, 't', 'a', 'b', 'l', 'e') // table
	buf = append(buf, 0x05, 't', 'a', 'b', 'l', 'e')
	buf = append(buf, byte(len(name)))
	buf = append(buf, []byte(name)...) // column name
	buf = append(buf, 0x0c, 0x3f, 0x00, 0x00, 0x00, 0xff, 0xff, 0xff, 0xff, 0x00, 0x00, 0x00, 0x00)
	return buf
}

func buildRowPacket(row []interface{}) []byte {
	var buf bytes.Buffer
	for _, val := range row {
		str := fmt.Sprintf("%v", val)
		buf.WriteByte(byte(len(str)))
		buf.WriteString(str)
	}
	return buf.Bytes()
}

func createHandshakePacket() []byte {
	var payload bytes.Buffer
	payload.WriteByte(0x0a)
	payload.WriteString("8.0.33-micy")
	payload.WriteByte(0x00)
	payload.Write([]byte{0x01, 0x00, 0x00, 0x00})
	payload.Write(make([]byte, 8))
	payload.WriteByte(0x00)
	payload.Write([]byte{0xff, 0xff})
	payload.WriteByte(0x21)
	payload.Write([]byte{0x00, 0x00})
	payload.WriteString("mysql_native_password")
	payload.WriteByte(0x00)
	return payload.Bytes()
}

func forwardQuery(sql string) (*SQLResponse, error) {
	req := SQLRequest{SQL: sql}
	data, _ := json.Marshal(req)

	httpReq, err := http.NewRequest("POST", lambdaURL, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("build http request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	res, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http request failed: %w", err)
	}
	defer res.Body.Close()

	var resp SQLResponse
	err = json.NewDecoder(res.Body).Decode(&resp)
	if err != nil {
		return nil, fmt.Errorf("decode error: %w", err)
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("sql error: %s", resp.Error)
	}
	return &resp, nil
}
