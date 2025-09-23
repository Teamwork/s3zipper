package main

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
	"context"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	cfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	redigo "github.com/garyburd/redigo/redis"
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

func main() {
	if 1 == 0 {
		test()
		return
	}

	configFile, err := os.Open("/go/src/s3zipper/conf.json")
	if err != nil {
		panic("Error opening conf.json: " + err.Error())
	}
	fmt.Printf("Opened configuration file 'conf.json'\n")

	decoder := json.NewDecoder(configFile)
	err = decoder.Decode(&configData)
	if err != nil {
		panic("Error reading conf: " + err.Error())
	}
	fmt.Printf("Loaded config: Bucket=%s, Region=%s, Redis=%s, Port=%d\n",
		configData.Bucket, configData.Region, configData.RedisServerAndPort, configData.Port)

	initAwsBucket()
	InitRedis()

	fmt.Printf("Starting HTTP server on port %d\n", configData.Port)
	http.HandleFunc("/", handler)
	err = http.ListenAndServe(":"+strconv.Itoa(configData.Port), nil)
	if err != nil {
		fmt.Printf("HTTP server failed: %s\n", err.Error())
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
		fmt.Printf("%s\n", err.Error())
	}

	parseFileDates(files)
	fmt.Printf("Test data parsed with %d files\n", len(files))
}

func parseFileDates(files []*RedisFile) {
	layout := "2006-01-02T15:04:05Z"
	for _, file := range files {
		t, err := time.Parse(layout, file.Modified)
		if err != nil {
			fmt.Printf("Error parsing date '%s' for file %s: %s\n", file.Modified, file.FileName, err.Error())
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
	fmt.Printf("AWS S3 client initialized\n")
}

func InitRedis() {
	redisPool = &redigo.Pool{
		MaxIdle:     10,
		IdleTimeout: 1 * time.Second,
		Dial: func() (redigo.Conn, error) {
			fmt.Printf("Connecting to Redis at %s\n", configData.RedisServerAndPort)
			return redigo.Dial("tcp", configData.RedisServerAndPort)
		},
		TestOnBorrow: func(c redigo.Conn, t time.Time) (err error) {
			if time.Since(t) < time.Minute {
				return nil
			}
			_, err = c.Do("PING")
			if err != nil {
				panic("Error connecting to redis")
			}
			return
		},
	}
	fmt.Printf("Redis connection pool initialized\n")
}

// Remove all other unrecognised characters apart from
var makeSafeFileName = regexp.MustCompile(`[#<>:"/\|?*\\]`)

func getFilesFromRedis(ref string) (files []*RedisFile, err error) {
	fmt.Printf("Fetching files from Redis with ref: %s\n", ref)

	// Testing - enable to test. Remove later.
	if 1 == 0 && ref == "test" {
		files = append(files, &RedisFile{FileName: "test.zip", Folder: "", S3Path: "test/test.zip"}) // Edit and duplicate line to test
		fmt.Printf("Returning test file data\n")
		return
	}

	redis := redisPool.Get()
	defer redis.Close()

	// Get the value from Redis
	result, err := redis.Do("GET", "zip:"+ref)
	if err != nil || result == nil {
		err = errors.New("Access Denied (sorry your link has timed out)")
		fmt.Printf("Redis GET failed or returned nil for ref: %s\n", ref)
		return
	}

	// Convert to bytes
	var resultByte []byte
	var ok bool
	if resultByte, ok = result.([]byte); !ok {
		err = errors.New("Error converting data stream to bytes")
		fmt.Printf("Type assertion to []byte failed for Redis data\n")
		return
	}

	// Decode JSON
	err = json.Unmarshal(resultByte, &files)
	if err != nil {
		err = errors.New("Error decoding json: " + string(resultByte))
		fmt.Printf("JSON Unmarshal error: %s\n", err.Error())
		return
	}
	fmt.Printf("Retrieved %d files from Redis\n", len(files))

	// Convert modified date strings to time objects
	parseFileDates(files)

	return
}

func handler(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	fmt.Printf("Handling request: %s %s\n", r.Method, r.RequestURI)

	health, ok := r.URL.Query()["health"]
	if len(health) > 0 {
		fmt.Fprintf(w, "OK")
		fmt.Printf("Health check responded OK\n")
		return
	}

	// Get "ref" URL params
	refs, ok := r.URL.Query()["ref"]
	if !ok || len(refs) < 1 {
		http.Error(w, "S3 File Zipper. Pass ?ref= to use.", 500)
		fmt.Printf("Missing ref parameter\n")
		return
	}
	ref := refs[0]
	fmt.Printf("Request ref: %s\n", ref)

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
	fmt.Printf("Download filename set to: %s\n", downloadas[0])

	files, err := getFilesFromRedis(ref)
	if err != nil {
		http.Error(w, err.Error(), 403)
		fmt.Printf("Error fetching files from Redis: %s\n", err.Error())
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
			fmt.Printf("Error closing zip writer: %s\n", err.Error())
		} else {
			fmt.Printf("Zip writer closed successfully\n")
		}
	}()

	for _, file := range files {
		if file.S3Path == "" {
			fmt.Printf("Skipping file with empty S3Path: %+v\n", file)
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

		fmt.Printf("Downloading file from S3: Bucket=%s, Key=%s\n", configData.Bucket, file.S3Path)
		resp, err := awsS3Client.GetObject(context.TODO(), input)
		if err != nil {
			var noKey *types.NoSuchKey
			if errors.As(err, &noKey) {
				fmt.Printf("File not found in S3: %s\n", file.S3Path)
			} else {
				fmt.Printf("Error downloading file \"%s\": %s\n", file.S3Path, err.Error())
			}
			continue
		}

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
			fmt.Printf("Error creating zip header for %s: %s\n", zipPath, err.Error())
			continue
		}

		fmt.Printf("Adding file to zip: %s\n", zipPath)

		_, err = io.Copy(f, resp.Body)
		resp.Body.Close()
		if err != nil {
			fmt.Printf("Error writing file %s to zip: %s\n", zipPath, err.Error())
			continue
		}
	}

	fmt.Printf("Request processed in %s\n", time.Since(start))
}
