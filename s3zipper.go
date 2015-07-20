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

	"net/http"

	"github.com/AdRoll/goamz/aws"
	"github.com/AdRoll/goamz/s3"
	redigo "github.com/garyburd/redigo/redis"
)

type Configuration struct {
	AccessKey          string
	SecretKey          string
	Bucket             string
	Region             string
	RedisServerAndPort string
	Port               int
}

var config = Configuration{}
var aws_bucket *s3.Bucket
var redisPool *redigo.Pool

func main() {

	configFile, _ := os.Open("conf.json")
	decoder := json.NewDecoder(configFile)
	err := decoder.Decode(&config)
	if err != nil {
		panic("Error reading conf")
	}

	initAwsBucket()
	InitRedis()

	fmt.Println("Running on port", config.Port)
	http.HandleFunc("/", handler)
	http.ListenAndServe(":"+strconv.Itoa(config.Port), nil)
}

func initAwsBucket() {
	now := time.Now()
	var dur time.Duration = time.Hour * 1
	expiration := now.Add(dur)

	auth, err := aws.GetAuth(config.AccessKey, config.SecretKey, "", expiration) //"" = token which isn't needed
	if err != nil {
		panic(err)
	}

	aws_bucket = s3.New(auth, aws.GetRegion(config.Region)).Bucket(config.Bucket)
}

func InitRedis() {
	redisPool = &redigo.Pool{
		MaxIdle:     10,
		IdleTimeout: 1 * time.Second,
		Dial: func() (redigo.Conn, error) {
			return redigo.Dial("tcp", config.RedisServerAndPort)
		},
		TestOnBorrow: func(c redigo.Conn, t time.Time) (err error) {
			_, err = c.Do("PING")
			if err != nil {
				panic("Error connecting to redis")
			}
			return
		},
	}
}

// Remove all other unrecognised characters apart from
var safeFileName = regexp.MustCompile(`[#<>:"/\|?*\\]`)

type TeamworkFile struct {
	ProjectId     int64
	ProjectName   string
	S3Path        string
	FileName      string
	FileVersionId int64
	Folder        string
}

func getFilesFromRedis(ref string) (twFiles []*TeamworkFile, err error) {
	redis := redisPool.Get()
	defer redis.Close()

	// Get the value from Redis
	result, err := redis.Do("GET", "zip:"+ref)
	if err != nil || result == nil {
		err = errors.New("Reference not found. Security violation logged")
		return
	}

	// Decode the JSON
	var resultByte []byte
	var ok bool
	if resultByte, ok = result.([]byte); !ok {
		fmt.Println("Error reading from redis")
	}
	err = json.Unmarshal(resultByte, &twFiles)
	if err != nil {
		// err = Errors.new("Reference not found. Security violation logged")
		panic("Error decoding twFiles redis data")
	}
	fmt.Println("Got twFiles", twFiles)
	for i, twFile := range twFiles {
		fmt.Println("twFiles", i, twFile.FileName)
	}

	return
}

func handler(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	values := r.URL.Query()
	values.Add("ref", "test")

	// Get "ref" URL params
	refs, ok := r.URL.Query()["ref"]
	if !ok || len(refs) < 1 {
		http.Error(w, "ref not passed", 500)
		return
	}
	ref := refs[0]

	// Get "downloadas" URL params
	downloadas, ok := r.URL.Query()["downloadas"]
	if !ok && len(downloadas) > 0 {
		downloadas[0] = safeFileName.ReplaceAllString(downloadas[0], "")
		if downloadas[0] == "" {
			downloadas[0] = "download.zip"
		}
	} else {
		downloadas = append(downloadas, "download.zip")
	}

	twFiles, err := getFilesFromRedis(ref)
	if err != nil {
		http.Error(w, "Reference not found. Security violation logged", 500)
		return
	}

	// Start processing the response
	w.Header().Add("Content-Disposition", "attachment; filename="+downloadas[0])
	w.Header().Add("Content-Type", "application/zip")

	// Loop over files, add them to the
	zipWriter := zip.NewWriter(w)
	for _, twFile := range twFiles {

		// Build Safe Project Name
		twFile.ProjectName = safeFileName.ReplaceAllString(twFile.ProjectName, "")
		if twFile.ProjectName == "" { // Unlikely but just in case
			twFile.ProjectName = "Project"
		}

		// Build safe file file name
		safeFileName := safeFileName.ReplaceAllString(twFile.FileName, "")
		if safeFileName == "" { // Unlikely but just in case
			safeFileName = "file"
		}

		fmt.Printf("Processing '%s'\n", twFile.S3Path)

		// Read file from S3
		rdr, err := aws_bucket.GetReader(twFile.S3Path)
		if err != nil {
			switch t := err.(type) {
			case *s3.Error:
				// skip non existing files
				if t.StatusCode == 404 {
					fmt.Println("S3 file not found!", twFile.S3Path)
					continue
				}
			}
			panic(err)
		}

		// Build a good path for the file within the zip
		zipPath := strconv.FormatInt(twFile.ProjectId, 10) + "." + twFile.ProjectName + "/"
		if twFile.Folder != "" {
			zipPath += twFile.Folder
			if strings.HasSuffix(zipPath, "/") {
				zipPath += "/"
			}
		}
		zipPath += strconv.FormatInt(twFile.FileVersionId, 10) + "." + safeFileName
		fmt.Printf("Adding to zip '%s'\n", zipPath)

		// Append to zip
		f, err := zipWriter.Create(zipPath)
		if err != nil {
			panic(err)
		}

		io.Copy(f, rdr)
		rdr.Close()
	}

	zipWriter.Close()

	log.Printf(
		"%s\t%s\t%s",
		r.Method,
		r.RequestURI,
		time.Since(start),
	)
}
