package main

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	cfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	redigo "github.com/gomodule/redigo/redis"
)

type Configuration struct {
	Bucket             string
	Region             string
	RedisServerAndPort string
	Port               int
}

var configData = Configuration{}
var redisPool *redigo.Pool
var awsS3Client *s3.Client

type RedisFile struct {
	FileName string
	Folder   string
	S3Path   string
	// Optional - we use are Teamwork.com but feel free to rmove
	FileId       int64 `json:",string"`
	ProjectId    int64 `json:",string"`
	ProjectName  string
	Modified     string
	ModifiedTime time.Time
}

var logger *slog.Logger

func initLogger() {
	logLevel := os.Getenv("LOG_LEVEL")
	var level slog.Level
	switch strings.ToLower(logLevel) {
	case "debug":
		level = slog.LevelDebug
	default:
		level = slog.LevelInfo
	}

	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: level,
	})
	logger = slog.New(handler)
	slog.SetDefault(logger)
}

func main() {
	if 1 == 0 {
		test()
		return
	}
	initLogger()

	configFilePath := os.Getenv("CONFIG_FILE")
	if configFilePath == "" {
		configFilePath = "/go/src/s3zipper/conf.json"
	}
	configFile, err := os.Open(configFilePath)

	if err != nil {
		panic("Error opening conf.json: " + err.Error())
	}
	slog.Info("Opened configuration file", "path", configFilePath)

	decoder := json.NewDecoder(configFile)
	err = decoder.Decode(&configData)
	if err != nil {
		panic("Error reading conf: " + err.Error())
	}
	slog.Info("Loaded configuration",
		"bucket", configData.Bucket,
		"region", configData.Region,
		"redis", configData.RedisServerAndPort,
		"port", configData.Port,
	)
	initAwsBucket()
	InitRedis()

	slog.Info("Starting HTTP server", "port", configData.Port)
	http.HandleFunc("/", handler)
	err = http.ListenAndServe(":"+strconv.Itoa(configData.Port), nil)
	if err != nil && err != http.ErrServerClosed {
		log.Fatalf("HTTP server error: %s", err)
	}
}

func test() {
	var err error
	var files []*RedisFile
	jsonData := "[{\"S3Path\":\"1\\/p23216.tf_A89A5199-F04D-A2DE-5824E635AC398956.Avis_Rent_A_Car_Print_Reservation.pdf\",\"FileVersionId\":\"4164\",\"FileName\":\"Avis Rent A Car_ Print Reservation.pdf\",\"ProjectName\":\"Superman\",\"ProjectId\":\"23216\",\"Folder\":\"\",\"FileId\":\"4169\"},{\"modified\":\"2015-07-18T02:05:04Z\",\"S3Path\":\"1\\/p23216.tf_351310E0-DF49-701F-60601109C2792187.a1.jpg\",\"FileVersionId\":\"4165\",\"FileName\":\"a1.jpg\",\"ProjectName\":\"Superman\",\"ProjectId\":\"23216\",\"Folder\":\"Level 1\\/Level 2 x\\/Level 3\",\"FileId\":\"4170\"}]"

	resultByte := []byte(jsonData)

	err = json.Unmarshal(resultByte, &files)
	if err != nil {
		err = errors.New("Error decoding json: " + jsonData)
		slog.Error(err.Error())
	}

	parseFileDates(files)
	slog.Info("Test data parsed", "file_count", len(files))
}

func parseFileDates(files []*RedisFile) {
	layout := "2006-01-02T15:04:05Z"
	for _, file := range files {
		t, err := time.Parse(layout, file.Modified)
		if err != nil {
			slog.Warn("Error parsing date", "date", file.Modified, "file", file.FileName, "error", err)
			continue
		}
		file.ModifiedTime = t
	}
}

func initAwsBucket() {
	awsCfg, err := cfg.LoadDefaultConfig(context.TODO(), cfg.WithRegion(configData.Region))
	if err != nil {
		log.Fatalf("unable to load SDK config, %v", err)
	}

	awsS3Client = s3.NewFromConfig(awsCfg)
	slog.Info("AWS S3 client initialized")
}

func InitRedis() {
	redisPool = &redigo.Pool{
		MaxIdle:     10,
		IdleTimeout: 1 * time.Second,
		Dial: func() (redigo.Conn, error) {
			slog.Info("Connecting to Redis", "addr", configData.RedisServerAndPort)
			return redigo.Dial("tcp", configData.RedisServerAndPort)
		},
		TestOnBorrow: func(c redigo.Conn, t time.Time) (err error) {
			if time.Since(t) < time.Minute {
				return nil
			}
			_, err = c.Do("PING")
			if err != nil {
				slog.Error("Error connecting to Redis", "error", err)
			}
			return
		},
	}
	slog.Info("Redis connection pool initialized")
}

// Remove all other unrecognised characters apart from
var makeSafeFileName = regexp.MustCompile(`[#<>:"/\|?*\\]`)

func getFilesFromRedis(ref string) (files []*RedisFile, err error) {
	slog.Debug("Fetching files from Redis", "ref", ref)

	// Testing - enable to test. Remove later.
	if 1 == 0 && ref == "test" {
		files = append(files, &RedisFile{FileName: "test.zip", Folder: "", S3Path: "test/test.zip"}) // Edit and duplicate line to test
		return
	}

	redis := redisPool.Get()
	defer redis.Close()

	// Get the value from Redis
	result, err := redis.Do("GET", "zip:"+ref)
	if err != nil || result == nil {
		err = errors.New("Access Denied (sorry your link has timed out)")
		slog.Warn("Redis GET failed or returned nil", "ref", ref)
		return
	}

	// Convert to bytes
	var resultByte []byte
	var ok bool
	if resultByte, ok = result.([]byte); !ok {
		err = errors.New("Error converting data stream to bytes")
		slog.Error("Type assertion to []byte failed for Redis data")
		return
	}

	// Decode JSON
	err = json.Unmarshal(resultByte, &files)
	if err != nil {
		err = errors.New("Error decoding json: " + string(resultByte))
		slog.Error("JSON Unmarshal error", "error", err)
		return
	}
	slog.Debug("Retrieved files from Redis", "count", len(files))

	// Convert modified date strings to time objects
	parseFileDates(files)

	return
}

func handler(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	slog.Debug("Handling request", "method", r.Method, "uri", r.RequestURI)

	health, ok := r.URL.Query()["health"]
	if len(health) > 0 {
		w.Write([]byte("OK"))
		slog.Debug("Health check responded OK")
		return
	}

	// Get "ref" URL params
	refs, ok := r.URL.Query()["ref"]
	if !ok || len(refs) < 1 {
		http.Error(w, "S3 File Zipper. Pass ?ref= to use.", 500)
		slog.Warn("Missing ref parameter")
		return
	}
	ref := refs[0]
	slog.Debug("Request ref", "ref", ref)

	// Get "downloadas" URL params
	downloadas, ok := r.URL.Query()["downloadas"]
	if !ok && len(downloadas) > 0 {
		downloadas[0] = makeSafeFileName.ReplaceAllString(downloadas[0], "")
		if downloadas[0] == "" {
			downloadas[0] = "download.zip"
		}
	} else {
		downloadas = append(downloadas, "download.zip")
	}
	slog.Debug("Download filename set", "filename", downloadas[0])

	files, err := getFilesFromRedis(ref)
	if err != nil {
		http.Error(w, err.Error(), 403)
		slog.Error("Error fetching files from Redis", "error", err)
		return
	}

	// Start processing the response
	w.Header().Add("Content-Disposition", "attachment; filename=\""+downloadas[0]+"\"")
	w.Header().Add("Content-Type", "application/zip")

	// Loop over files, add them to the zip archive
	zipWriter := zip.NewWriter(w)
	defer func() {
		err := zipWriter.Close()
		if err != nil {
			slog.Error("Error closing zip writer", "error", err)
		} else {
			slog.Debug("Zip writer closed successfully")
		}
	}()

	for _, file := range files {
		if file.S3Path == "" {
			slog.Warn("Skipping file with empty S3Path", "file", file)
			continue
		}

		// Build safe file name
		safeFileName := makeSafeFileName.ReplaceAllString(file.FileName, "")
		if safeFileName == "" { // Unlikely but just in case
			safeFileName = "file"
		}

		// Read file from S3, log any errors
		input := &s3.GetObjectInput{
			Bucket: aws.String(configData.Bucket),
			Key:    aws.String(file.S3Path),
		}

		slog.Debug("Downloading file from S3", "bucket", configData.Bucket, "key", file.S3Path)
		resp, err := awsS3Client.GetObject(context.TODO(), input)
		if err != nil {
			var noKey *types.NoSuchKey
			if errors.As(err, &noKey) {
				slog.Warn("File not found in S3", "s3_path", file.S3Path)
			} else {
				slog.Error("Error downloading file", "s3_path", file.S3Path, "error", err)
			}
			continue
		}
		defer resp.Body.Close()

		// Build path for file within the zip
		zipPath := ""
		if file.ProjectId > 0 {
			zipPath += strconv.FormatInt(file.ProjectId, 10) + "."
			file.ProjectName = makeSafeFileName.ReplaceAllString(file.ProjectName, "")
			if file.ProjectName == "" {
				file.ProjectName = "Project"
			}
			zipPath += file.ProjectName + "/"
		}
		if file.Folder != "" {
			zipPath += file.Folder
			if !strings.HasSuffix(zipPath, "/") {
				zipPath += "/"
			}
		}
		zipPath += safeFileName

		h := &zip.FileHeader{
			Name:   zipPath,
			Method: zip.Deflate,
			Flags:  0x800,
		}

		if file.Modified != "" {
			h.SetModTime(file.ModifiedTime)
		}

		f, err := zipWriter.CreateHeader(h)
		if err != nil {
			slog.Error("Error creating zip header", "zip_path", zipPath, "error", err)
			continue
		}

		slog.Debug("Adding file to zip", "zip_path", zipPath)

		_, err = io.Copy(f, resp.Body)
		if err != nil {
			slog.Error("Error writing file to zip", "zip_path", zipPath, "error", err)
			continue
		}
	}

	slog.Debug("Request processed", "duration", time.Since(start))
}
