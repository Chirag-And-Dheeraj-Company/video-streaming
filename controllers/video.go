package controllers

import (
	"crypto/sha1"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
	. "video-streaming-server/types"
	"video-streaming-server/utils"
)

func closeVideoFile(tmpFile *os.File) {
	err := tmpFile.Close()

	if err != nil {
		log.Fatal(err)
	}

	err = os.Remove(tmpFile.Name())

	if err != nil {
		log.Fatal(err)
	}
}

func UploadVideo(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	fileName := r.Header.Get("file-name")
	isFirstChunk := r.Header.Get("first-chunk")
	fileSize, _ := strconv.Atoi(r.Header.Get("file-size"))

	serverFileName := fileName + ".mp4"

	if isFirstChunk == "true" {
		title := r.Header.Get("title")
		description := r.Header.Get("description")
		log.Println("Started receiving chunks for: " + fileName)
		log.Println("Size of the file received:", fileSize)
		log.Println("Title: ", title)
		log.Println("Description: ", description)
		log.Println("Creating a database record...")

		insertStatement, err := db.Prepare(`
			INSERT INTO 
				videos
					(
						video_id,
						title,
						description,
						upload_initiate_time,
						upload_status,
						upload_end_time
					) 
				VALUES 
					($1,$2,$3,$4,$5,$6)
		`)

		if err != nil {
			log.Fatal(err)
		}

		_, err = insertStatement.Exec(fileName, title, description, time.Now(), 0, time.Now())

		if err != nil {
			log.Fatal(err)
		} else {
			log.Print("Database record created.")
		}
	}

	d, _ := io.ReadAll(r.Body)

	var tmpFile *os.File
	var err error

	if isFirstChunk == "true" {
		tmpFile, err = os.Create("./video/" + serverFileName)
		if err != nil {
			log.Println("Error creating file:", err)
			http.Error(w, "Error creating file", http.StatusInternalServerError)
			return
		}
	} else {
		tmpFile, err = os.OpenFile("./video/"+serverFileName, os.O_APPEND|os.O_WRONLY, os.ModeAppend)
		if err != nil {
			log.Println("Error opening file:", err)
			http.Error(w, "Error opening file", http.StatusInternalServerError)
			return
		}
	}

	_, err = tmpFile.Write(d)

	if err != nil {
		log.Println("Error writing to file:", err)
		http.Error(w, "Error writing to file", http.StatusInternalServerError)
		return
	}

	fileInfo, err := tmpFile.Stat()

	if err != nil {
		log.Println("Error getting file info:", err)
		http.Error(w, "Error getting file info", http.StatusInternalServerError)
		return
	}

	if fileInfo.Size() == int64(fileSize) {
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte("File received completely."))
		log.Println("Received all chunks for: " + serverFileName)
		log.Println("Breaking the video into .ts files.")

		breakResult := utils.BreakFile(("./video/" + serverFileName), fileName)

		if breakResult {
			log.Println("Successfully broken " + fileName + " into .ts files.")
			log.Println("Deleting the original file from server.")
			closeVideoFile(tmpFile)
			utils.UploadToAppwrite(fileName, db)
			log.Println("Successfully uploaded chunks of", fileName, "to Appwrite Storage")
		} else {
			log.Println("Error breaking " + fileName + " into .ts files.")
		}
	} else {
		w.WriteHeader(http.StatusPartialContent)
		w.Write([]byte("Receiving chunks of the file."))
	}
}

func GetVideos(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	log.Println("Querying the database now for a list of videos...")
	rows, err := db.Query(`
		SELECT 
			video_id,
			title,
			description
		FROM
			videos;
	`)

	if err != nil {
		log.Fatal(err)
	}

	defer rows.Close()

	log.Println("Query executed.")

	records := make([]Video, 0)

	for rows.Next() {
		var id string
		var title string
		var description string

		err := rows.Scan(&id, &title, &description)

		if err != nil {
			log.Fatal(err)
		}

		record := Video{
			ID:          id,
			Title:       title,
			Description: description,
		}

		records = append(records, record)
	}

	recordsJSON, err := json.Marshal(records)

	if err != nil {
		log.Fatal(err)
	} else {
		fmt.Fprint(w, string(recordsJSON))
	}
}

func GetVideo(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	video_id := r.URL.Path[len("/video/"):]
	log.Println("Details of " + video_id + " requested.")
	detailsQuery, err := db.Prepare(`
		SELECT
			title, description
		FROM
			videos
		WHERE
			video_id=$1;
	`)

	if err != nil {
		log.Fatal(err)
	}

	defer detailsQuery.Close()

	var title, description string
	err = detailsQuery.QueryRow(video_id).Scan(&title, &description)

	if err != nil {
		log.Fatal(err)
	}

	log.Println("Video ID: " + video_id)
	log.Println("Title: " + title)
	log.Println("Description: " + description)
	videoDetails := &Video{
		ID:          video_id,
		Title:       title,
		Description: description,
	}
	videoDetailsJSON, err := json.Marshal(videoDetails)

	if err != nil {
		log.Fatal(err)
	}

	fmt.Fprint(w, string(videoDetailsJSON))
}

func GetManifestFile(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	videoId := strings.Split(r.URL.Path[1:], "/")[1]

	log.Println("Video ID: " + videoId)

	getManifestFile := "https://cloud.appwrite.io/v1/storage/buckets/" + os.Getenv("BUCKET_ID") + "/files/" + videoId + "/view"

	request, err := http.NewRequest("GET", getManifestFile, nil)

	if err != nil {
		log.Fatal(err)
	}

	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Appwrite-Response-Format", os.Getenv("APPWRITE_RESPONSE_FORMAT"))
	request.Header.Set("X-Appwrite-Project", os.Getenv("APPWRITE_PROJECT_ID"))
	request.Header.Set("X-Appwrite-Key", os.Getenv("APPWRITE_KEY"))

	client := &http.Client{}

	response, err := client.Do(request)

	if err != nil {
		log.Fatal(err)
	}

	defer response.Body.Close()

	bodyBytes, err := io.ReadAll(response.Body)

	if err != nil {
		log.Fatal(err)
	}

	w.Header().Set("Content-Type", "application/x-mpegURL")
	w.Write(bodyBytes)
}

func GetTSFiles(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	videoName := strings.Split(r.URL.Path[1:], "/")[3]

	videoComps := strings.Split(videoName, ".")
	hashChecksum := sha1.New()
	hashChecksum.Write([]byte(videoComps[0]))
	fileId := fmt.Sprintf("%x", hashChecksum.Sum(nil))[:36]

	log.Println("Video chunk requested: " + fileId)

	getSegmentFile := "https://cloud.appwrite.io/v1/storage/buckets/" + os.Getenv("BUCKET_ID") + "/files/" + fileId + "/view"

	request, err := http.NewRequest("GET", getSegmentFile, nil)

	if err != nil {
		log.Fatal(err)
	}

	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Appwrite-Response-Format", os.Getenv("APPWRITE_RESPONSE_FORMAT"))
	request.Header.Set("X-Appwrite-Project", os.Getenv("APPWRITE_PROJECT_ID"))
	request.Header.Set("X-Appwrite-Key", os.Getenv("APPWRITE_KEY"))

	client := &http.Client{}

	response, err := client.Do(request)
	if err != nil {
		log.Fatal(err)
	}
	defer response.Body.Close()

	bodyBytes, err := io.ReadAll(response.Body)

	if err != nil {
		log.Fatal(err)
	}

	w.Header().Set("Content-Type", "video/MP2T")
	w.Write(bodyBytes)
}
