package main

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
)

type execRequest struct {
	Id                 string  `json:"id"`
	Data               []byte  `json:"data"`
	TargetActor        int     `json:"targetActor"`
	PtList             []int   `json:"ptList"`
	ExtraPer1000       float64 `json:"extraPer1000"`
	DeviationThreshold float64 `json:"deviationThreshold"`
}

var isRunning bool = false
var runningParams execRequest
var taskChan = make(chan execRequest)

var workingDirectory = "/home/liangxinyun/akochan-reviewer"
var executablePath = "/home/liangxinyun/akochan-reviewer/target/release/akochan-reviewer"
var outputDirectory = "/home/liangxinyun/akochan-output"
var inputDirectory = "/home/liangxinyun/akochan-input"

func main() {
	go worker()

	r := gin.Default()
	r.POST("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message": "pong",
		})
	})
	r.GET("/status", getStatus)
	r.POST("/start", start)
	r.Run() // listen and serve on 0.0.0.0:8080 (for windows "localhost:8080")
}

func getStatus(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"isRunning": isRunning,
		"params":    runningParams,
	})
}

func start(c *gin.Context) {
	var req execRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}
	select {
	case taskChan <- req:
		c.JSON(http.StatusOK, gin.H{
			"message": "started",
		})
	default:
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "already running",
		})
	}
}

func worker() {
	for {
		req := <-taskChan
		log.Printf("Start processing request %s", req.Id)
		isRunning = true
		runningParams = req
		var wg sync.WaitGroup
		var err error
		wg.Add(1)
		go func() {
			defer wg.Done()
			err = analyze(req)
			if err != nil {
				log.Println(err.Error())
			}
		}()
		wg.Wait()
		// the err is reserved for grpc refactor
		_ = err
		isRunning = false
		runningParams = execRequest{}
	}
}

func analyze(req execRequest) error {
	outputPath := path.Join(outputDirectory, req.Id+".html")
	fi, err := os.Stat(outputPath)
	if err == nil {
		// file exists
		if fi.Size() != 0 {
			// result exists, skip.
			return nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	log.Println("Completed checking file exist")

	inputPath := path.Join(inputDirectory, req.Id+".json")
	err = os.WriteFile(inputPath, req.Data, 0644)
	if err != nil {
		return err
	}

	env := append(os.Environ(), "LD_LIBRARY_PATH="+path.Join(workingDirectory, "akochan"))
	ptListStr := make([]string, len(req.PtList))
	for i, pt := range req.PtList {
		ptListStr[i] = strconv.Itoa(pt)
	}
	args := []string{
		"-a", fmt.Sprintf("%d", req.TargetActor),
		"--pt", strings.Join(ptListStr, ","),
		"-n", fmt.Sprintf("%f", req.DeviationThreshold),
		"-o", outputPath,
		"-i", inputPath,
		"--no-open",
	}
	log.Println(args)

	// create directory if not exists
	err = os.MkdirAll(outputDirectory, os.ModePerm)
	if err != nil {
		return err
	}

	log.Println("Ready to run")
	cmd := exec.Command(executablePath, args...)
	cmd.Dir = workingDirectory
	cmd.Env = env
	err = cmd.Start()
	if err != nil {
		return err
	}
	err = cmd.Wait()
	if err != nil {
		return err
	}
	return nil
}
