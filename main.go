package main

import (
	"database/sql"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

func main() {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s",
		os.Getenv("DB_USER"),
		os.Getenv("DB_PASS"),
		os.Getenv("DB_HOST"),
		os.Getenv("DB_PORT"),
		os.Getenv("DB_NAME"))

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		log.Fatalf("ไม่สามารถเชื่อมต่อกับฐานข้อมูลได้: %v", err)
	}
	defer db.Close()

	db.SetConnMaxLifetime(time.Minute * 3)
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(10)

	err = db.Ping()
	if err != nil {
		log.Fatalf("ไม่สามารถเชื่อมต่อกับฐานข้อมูลได้: %v", err)
	}
	fmt.Println("เชื่อมต่อกับ MariaDB สำเร็จ!")

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	var currentURLs []URLData

	var urlMutex sync.RWMutex

	updateURLs := func() {
		urls, err := fetchURLs(db)
		if err != nil {
			log.Printf("เกิดข้อผิดพลาดในการดึงข้อมูล URL: %v", err)
			return
		}

		urlMutex.Lock()
		defer urlMutex.Unlock()

		if len(urls) == 0 {
			log.Println("ไม่พบ URL ในฐานข้อมูล")
			return
		}

		if len(currentURLs) != len(urls) {
			log.Printf("พบการเปลี่ยนแปลงข้อมูล URL: เดิม %d รายการ, ใหม่ %d รายการ",
				len(currentURLs), len(urls))
			currentURLs = urls
			return
		}

		urlMap := make(map[int]string)
		for _, url := range currentURLs {
			urlMap[url.ID] = url.URL
		}

		hasChanges := false
		for _, url := range urls {
			storedURL, exists := urlMap[url.ID]
			if !exists || storedURL != url.URL {
				hasChanges = true
				break
			}
		}

		if hasChanges {
			log.Println("พบการเปลี่ยนแปลงข้อมูล URL")
			currentURLs = urls
		}
	}

	updateURLs()

	if len(currentURLs) == 0 {
		log.Println("ไม่พบ URL ในฐานข้อมูล รอข้อมูลก่อนเริ่มต้น...")
	} else {
		log.Printf("เริ่มต้นด้วย URL %d รายการ", len(currentURLs))
	}

	// ตั้งค่า ticker สำหรับยิง request ทุก 30 วินาที
	ticker := time.NewTicker(30 * time.Second)

	// ทำงานตลอดไป
	for range ticker.C {
		// อัปเดตข้อมูล URL ล่าสุดจากฐานข้อมูลก่อนยิง request
		updateURLs()

		urlMutex.RLock()
		urlCount := len(currentURLs)
		urlMutex.RUnlock()

		if urlCount == 0 {
			log.Println("ไม่มี URL ในฐานข้อมูล รอรอบถัดไป...")
			continue
		}

		log.Printf("====== เริ่มยิง HTTP GET request ไปยัง %d URLs ======", urlCount)

		var wg sync.WaitGroup

		urlMutex.RLock()
		for _, urlData := range currentURLs {
			wg.Add(1)
			go func(id int, url string) {
				defer wg.Done()
				requestURL(client, id, url)
			}(urlData.ID, urlData.URL)
		}
		urlMutex.RUnlock()

		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			log.Println("ดำเนินการยิง request ทั้งหมดเสร็จสิ้น")
		case <-time.After(15 * time.Second):
			log.Println("บาง request อาจยังไม่เสร็จสิ้น แต่เราจะดำเนินการต่อ")
		}
	}
}

type URLData struct {
	ID  int
	URL string
}

func fetchURLs(db *sql.DB) ([]URLData, error) {
	rows, err := db.Query("SELECT id, url FROM urls")
	if err != nil {
		return nil, fmt.Errorf("ไม่สามารถค้นหาข้อมูลได้: %v", err)
	}
	defer rows.Close()

	var urls []URLData
	for rows.Next() {
		var urlData URLData
		if err := rows.Scan(&urlData.ID, &urlData.URL); err != nil {
			return nil, fmt.Errorf("ไม่สามารถอ่านข้อมูลได้: %v", err)
		}
		urls = append(urls, urlData)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("เกิดข้อผิดพลาดระหว่างการอ่านข้อมูล: %v", err)
	}

	return urls, nil
}

func requestURL(client *http.Client, id int, url string) {
	startTime := time.Now()

	resp, err := client.Get(url)
	if err != nil {
		log.Printf("ID %d - ไม่สามารถเข้าถึง URL %s ได้: %v", id, url, err)
		return
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Printf("ID %d - ไม่สามารถอ่านข้อมูลจาก URL %s ได้: %v", id, url, err)
		return
	}

	duration := time.Since(startTime)
	log.Printf("ID %d - ยิง GET ไปที่ %s สำเร็จ - สถานะ: %d, ขนาด: %d bytes, เวลา: %v",
		id, url, resp.StatusCode, len(body), duration)
}
