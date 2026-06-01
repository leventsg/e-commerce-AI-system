package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type APIManager struct {
	rootPath  string
	mu        sync.Mutex
	processes []*exec.Cmd
}

func NewAPIManager(rootPath string) *APIManager {
	return &APIManager{rootPath: rootPath}
}

var APIs []string

func (am *APIManager) startAPIs(dirName, ext string) error {
	dirPath := filepath.Join(am.rootPath, dirName)
	dirs, err := os.ReadDir(dirPath)
	if err != nil {
		return err
	}

	for _, entry := range dirs {
		if !entry.IsDir() {
			continue
		}
		if !slices.Contains(APIs, entry.Name()) {
			continue
		}

		apiName := entry.Name()
		dir := filepath.Join(dirPath, apiName)
		fileName := fmt.Sprintf("%s.%s", apiName, ext)
		cmd := exec.Command("go", "run", fileName)
		cmd.Dir = dir
		setProcessGroup(cmd)

		logDir := filepath.Join(am.rootPath, "dev_logs")
		if err := os.MkdirAll(logDir, 0755); err != nil {
			return err
		}

		logFile, err := os.Create(filepath.Join(logDir, fmt.Sprintf("%s-api.log", apiName)))
		if err != nil {
			fmt.Printf("Error creating log file for %s: %v\n", apiName, err)
			continue
		}

		cmd.Stdout = logFile
		cmd.Stderr = logFile
		if err := cmd.Start(); err != nil {
			_ = logFile.Close()
			fmt.Printf("Error running %s %s: %v\n", dirName, apiName, err)
			continue
		}

		am.addProcess(cmd)
		go func(name string, cmd *exec.Cmd, logFile *os.File) {
			_ = cmd.Wait()
			_ = logFile.Close()
			am.removeProcess(cmd)
		}(apiName, cmd, logFile)

		fmt.Printf("Started %s API, logs: dev_logs/%s-api.log\n", apiName, apiName)
	}

	return nil
}

func (am *APIManager) addProcess(cmd *exec.Cmd) {
	am.mu.Lock()
	defer am.mu.Unlock()
	am.processes = append(am.processes, cmd)
}

func (am *APIManager) removeProcess(cmd *exec.Cmd) {
	am.mu.Lock()
	defer am.mu.Unlock()

	for i, item := range am.processes {
		if item == cmd {
			am.processes = append(am.processes[:i], am.processes[i+1:]...)
			return
		}
	}
}

func (am *APIManager) stopProcesses() {
	am.mu.Lock()
	processes := append([]*exec.Cmd(nil), am.processes...)
	am.mu.Unlock()

	for _, cmd := range processes {
		killProcessTree(cmd)
	}
}

func (am *APIManager) handleSignals() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	for sig := range sigCh {
		fmt.Printf("Received signal: %s\n", sig)
		am.stopProcesses()
		os.Exit(0)
	}
}

func setProcessGroup(cmd *exec.Cmd) {
}

func killProcessTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}

	pid := cmd.Process.Pid
	if runtime.GOOS == "windows" {
		_ = exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(pid)).Run()
		return
	}

	_ = cmd.Process.Signal(os.Interrupt)
	time.Sleep(500 * time.Millisecond)
	_ = cmd.Process.Kill()
}

func main() {
	root, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	var apis string
	var apiSet []string
	apisPath := filepath.Join(root, "apis")
	apiDir, err := os.ReadDir(apisPath)
	if err != nil {
		panic(err)
	}
	for _, api := range apiDir {
		if api.IsDir() {
			apiSet = append(apiSet, api.Name())
		}
	}

	flag.StringVar(&apis, "apis", "", fmt.Sprintf("choose APIs to run: %v", strings.Join(apiSet, ",")))
	flag.Parse()

	if apis != "" {
		APIs = strings.Split(apis, ",")
		for _, api := range APIs {
			if !slices.Contains(apiSet, api) {
				log.Fatalf("Invalid API name: %s. Available APIs: %v", api, apiSet)
			}
		}
	} else {
		APIs = apiSet
	}

	log.Println("you will run APIs:", APIs)
	am := NewAPIManager(root)
	if err := am.startAPIs("apis", "go"); err != nil {
		panic(err)
	}
	am.handleSignals()
}
