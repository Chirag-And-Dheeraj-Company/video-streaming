package utils

import (
	"bufio"
	"bytes"
	"crypto/sha1"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
	"video-streaming-server/config"
	"video-streaming-server/database"
	"video-streaming-server/repositories"
	"video-streaming-server/types"

	"github.com/golang-jwt/jwt/v5"
)

func LoadEnvVars() {
	log.Println("Setting environment variables...")

	envFile, err := os.Open(".env")

	if err != nil {
		log.Fatal(err)
	}

	defer envFile.Close()

	scanner := bufio.NewScanner(envFile)

	for scanner.Scan() {
		lineSplit := strings.Split(scanner.Text(), "=")
		os.Setenv(lineSplit[0], lineSplit[1])
	}

	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}

	log.Println("Environment variables set.")
}

func breakFile(videoPath string, fileName string) bool {
	log.Println("Inside BreakFile function.")

	if err := os.Mkdir(fmt.Sprintf("segments/%s", fileName), os.ModePerm); err != nil {
		log.Fatal(err)
	}

	log.Println("Created directory inside segments folder.")

	log.Println("Video path: " + videoPath)

	metaData, err := extractMetaData(videoPath)
	videoCodec := ""
	audioCodec := ""
	if err != nil {
		log.Println("Error extracting metadata for:" + videoPath)
	} else {
		videoCodec = metaData.Streams[0].CodecName
		audioCodec = metaData.Streams[1].CodecName
		log.Println("Video Codec: " + videoCodec)
		log.Println("Audio Codec: " + audioCodec)
	}

	videoCodecAction := "copy"
	audioCodecAction := "copy"

	if videoCodec != "h264" {
		log.Println("converting video codec to AVC")
		videoCodecAction = "libx264"
	}

	if audioCodec != "aac" {
		log.Println("converting audio codec to AAC")
		audioCodecAction = "aac"
	}

	cmd := exec.Command("ffmpeg", "-y", "-i", videoPath, "-c:v", videoCodecAction, "-c:a", audioCodecAction, "-map", "0", "-f", "segment", "-segment_time", "4", "-segment_format", "mpegts", "-segment_list", os.Getenv("ROOT_PATH")+"/segments/"+fileName+"/"+fileName+".m3u8", "-segment_list_type", "m3u8", os.Getenv("ROOT_PATH")+"/segments/"+fileName+"/"+fileName+"_"+"segment_no_%d.ts")

	output, err := cmd.CombinedOutput()

	if err != nil {
		log.Println(output)
		log.Println(err)
		return false
	} else {
		return true
	}
}

func ResumeUploadIfAny(db *sql.DB) {
	folders, err := os.ReadDir("segments")

	if err != nil {
		log.Fatal(err)
	}

	for _, folder := range folders {
		uploadToAppwrite(folder.Name(), db)
	}
}

func uploadToAppwrite(folderName string, db *sql.DB) {
	files, err := os.ReadDir(fmt.Sprintf("segments/%s", folderName))

	if err != nil {
		log.Fatal(err)
	}

	if len(files) == 0 {
		err = os.Remove("segments/" + folderName)
		if err != nil {
			log.Fatal(err)
		}
		return
	}

	log.Println("Now uploading chunks of " + folderName + " to Appwrite Storage...")
	var count int = -1
	for idx, file := range files {
		fileToUpload, err := os.ReadFile(fmt.Sprintf("segments/%s/%s", folderName, file.Name()))

		if err != nil {
			log.Fatal(err)
		}

		uploadRequestURL := "https://cloud.appwrite.io/v1/storage/buckets/" + os.Getenv("BUCKET_ID") + "/files"

		var requestBody bytes.Buffer
		writer := multipart.NewWriter(&requestBody)

		fileId := "nil"
		fileComps := strings.Split(file.Name(), ".")
		if fileComps[1] == "m3u8" {
			fileId = fileComps[0]
		} else {
			fileId = GetFileId(fileComps[0])
		}

		err = writer.WriteField("fileId", fileId)

		if err != nil {
			log.Fatal(err)
		}

		part, err := writer.CreateFormFile("file", file.Name())

		if err != nil {
			log.Fatal(err)
		}

		_, err = part.Write(fileToUpload)

		if err != nil {
			log.Fatal(err)
		}

		err = writer.Close()

		if err != nil {
			log.Fatal(err)
		}

		request, err := http.NewRequest("POST", uploadRequestURL, &requestBody)
		if err != nil {
			log.Printf("Error creating request")
			log.Fatal(err)
		}

		request.Header.Set("Content-Type", writer.FormDataContentType())
		request.Header.Set("X-Appwrite-Response-Format", os.Getenv("APPWRITE_RESPONSE_FORMAT"))
		request.Header.Set("X-Appwrite-Project", os.Getenv("APPWRITE_PROJECT_ID"))
		request.Header.Set("X-Appwrite-Key", os.Getenv("APPWRITE_KEY"))

		client := &http.Client{}
		response, err := client.Do(request)
		if err != nil {
			log.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != 201 {
			body, err := io.ReadAll(response.Body)
			if err != nil {
				log.Fatal(err)
			}
			log.Fatal(string(body))
		} else {
			count = idx
			err = os.Remove("segments/" + folderName + "/" + file.Name())
			if err != nil {
				log.Fatal(err)
			}
		}
	}

	log.Println("Updating upload status in database record...")
	updateStatement, err := db.Prepare(`
	UPDATE
		videos
	SET
		upload_status=$1,
		upload_end_time=$2
		WHERE
		video_id=$3;
	`)

	if err != nil {
		log.Fatal(err)
	}

	_, err = updateStatement.Exec(1, time.Now(), folderName)

	if err != nil {
		log.Fatal(err)
	} else {
		log.Println("Database record updated.")
		log.Println("Finished uploading", folderName, " :)")
	}

	if count == len(files)-1 {
		err = os.Remove("segments/" + folderName)
		if err != nil {
			log.Fatal(err)
		}
	}
}

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

func PostUploadProcessFile(serverFileName string, fileName string, tmpFile *os.File, db *sql.DB) {
	log.Println("Received all chunks for: " + serverFileName)
	log.Println("Breaking the video into .ts files.")

	breakResult := breakFile(("./video/" + serverFileName), fileName)
	if breakResult {
		log.Println("Successfully broken " + fileName + " into .ts files.")
		log.Println("Deleting the original file from server.")
		closeVideoFile(tmpFile)
		uploadToAppwrite(fileName, db)
		log.Println("Successfully uploaded chunks of", fileName, "to Appwrite Storage")
	} else {
		log.Println("Error breaking " + fileName + " into .ts files.")
	}
}

func GetManifestFile(w http.ResponseWriter, videoId string) ([]byte, error) {
	log.Println("Video ID: " + videoId)

	getManifestFile := "https://cloud.appwrite.io/v1/storage/buckets/" + os.Getenv("BUCKET_ID") + "/files/" + videoId + "/view"

	request, err := http.NewRequest("GET", getManifestFile, nil)

	if err != nil {
		return nil, err
	}

	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Appwrite-Response-Format", os.Getenv("APPWRITE_RESPONSE_FORMAT"))
	request.Header.Set("X-Appwrite-Project", os.Getenv("APPWRITE_PROJECT_ID"))
	request.Header.Set("X-Appwrite-Key", os.Getenv("APPWRITE_KEY"))

	client := &http.Client{}

	response, err := client.Do(request)

	if err != nil {
		return nil, err
	}

	defer response.Body.Close()

	if response.StatusCode == 404 {
		http.Error(w, "Video record not found", http.StatusNotFound)
		return nil, err
	}

	bodyBytes, err := io.ReadAll(response.Body)

	if err != nil {
		return nil, err
	}

	return bodyBytes, nil
}

func GetFileId(fileName string) string {
	hashChecksum := sha1.New()
	hashChecksum.Write([]byte(fileName))
	fileId := fmt.Sprintf("%x", hashChecksum.Sum(nil))[:36]

	return fileId
}

func DeleteVideo(w http.ResponseWriter, r *http.Request, db *sql.DB, videoId string) {
	fileBytes, err := GetManifestFile(w, videoId)

	if err != nil {
		log.Println(err)
		http.Error(w, "Error Deleting Video", http.StatusInternalServerError)
		return
	}

	file := string(fileBytes)
	lines := strings.Split(file, "\n")

	deleteUrl := "https://cloud.appwrite.io/v1/storage/buckets/" + os.Getenv("BUCKET_ID") + "/files/"

	for i := 0; i < len(lines); i++ {
		if strings.HasSuffix(lines[i], ".ts") {
			fileName := strings.Split(lines[i], ".")[0]
			fileId := GetFileId(fileName)

			request, err := http.NewRequest("DELETE", deleteUrl+fileId, nil)

			if err != nil {
				log.Println(err)
				http.Error(w, "Error Deleting Chunk", http.StatusInternalServerError)
				return
			}

			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("X-Appwrite-Response-Format", os.Getenv("APPWRITE_RESPONSE_FORMAT"))
			request.Header.Set("X-Appwrite-Project", os.Getenv("APPWRITE_PROJECT_ID"))
			request.Header.Set("X-Appwrite-Key", os.Getenv("APPWRITE_KEY"))

			client := &http.Client{}

			response, err := client.Do(request)
			if err != nil {
				log.Println(err)
				http.Error(w, "Error Deleting Chunk", http.StatusInternalServerError)
				return
			}
			defer response.Body.Close()
		}
	}

	log.Println("Deleted all .ts files...")

	request, err := http.NewRequest("DELETE", deleteUrl+videoId, nil)

	if err != nil {
		log.Println(err)
		http.Error(w, "Error Deleting Video", http.StatusInternalServerError)
		return
	}

	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Appwrite-Response-Format", os.Getenv("APPWRITE_RESPONSE_FORMAT"))
	request.Header.Set("X-Appwrite-Project", os.Getenv("APPWRITE_PROJECT_ID"))
	request.Header.Set("X-Appwrite-Key", os.Getenv("APPWRITE_KEY"))

	client := &http.Client{}

	response, err := client.Do(request)
	if err != nil {
		log.Println(err)
		http.Error(w, "Error Deleting Video", http.StatusInternalServerError)
		return
	}
	defer response.Body.Close()

	log.Println("Deleted .m3u8 file...")

	query, err := db.Prepare(`DELETE FROM videos WHERE video_id=$1`)

	if err != nil {
		log.Fatal(err)
	}

	_, err = query.Exec(videoId)

	if err != nil {
		log.Println(err)
		http.Error(w, "Error deleting record", http.StatusInternalServerError)
		return
	}

	log.Println("Deleted database record...")
}

func GenerateJWT(userID string, username string) (string, error) {
	claims := jwt.MapClaims{
		"user_id":  userID,
		"username": username,
		"exp":      time.Now().Add(time.Hour * 72).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	return token.SignedString([]byte(config.SecretKey))
}

func DecodeJWT(tokenString string) (jwt.MapClaims, error) {
	claims := jwt.MapClaims{}

	_, _, err := jwt.NewParser().ParseUnverified(tokenString, claims)
	if err != nil {
		return nil, err
	}

	// Verify expiration
	exp, ok := claims["exp"].(float64)
	if !ok {
		return nil, errors.New("expiration claim missing")
	}
	if time.Since(time.Unix(int64(exp), 0)) > 0 {
		return nil, errors.New("token expired")
	}

	return claims, nil
}

func VerifyToken(tokenString string) (*jwt.Token, error) {
	return jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(config.SecretKey), nil
	})
}

func GetUserFromRequest(r *http.Request) (*types.User, error) {
	authToken, err := r.Cookie("auth_token")
	if err != nil {
		return nil, err
	}
	db := database.GetDBConn()
	userRepository := repositories.NewUserRepository(db)
	token, err := VerifyToken(authToken.Value)
	if err != nil {
		return nil, err
	}
	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		userID := claims["user_id"].(string)
		user, err := userRepository.GetUserByID(userID)
		if err != nil {
			return nil, err
		}
		return user, nil
	}
	return nil, nil
}

func extractMetaData(videoPath string) (*types.FFProbeOutput, error) {
	log.Print(videoPath)
	cmd := exec.Command("ffprobe",
		"-v", "error",
		"-show_entries", "stream=codec_name,codec_type",
		"-show_entries", "format=filename,duration,bit_rate,size",
		"-of", "json",
		videoPath,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("error running ffprobe: %w", err)
	}

	var ffprobeOutput types.FFProbeOutput
	err = json.Unmarshal(output, &ffprobeOutput)
	if err != nil {
		return nil, fmt.Errorf("error parsing ffprobe output: %w", err)
	}

	return &ffprobeOutput, nil
}

func SendError(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}
