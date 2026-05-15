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

type ServiceManager struct {
	rootPath  string
	mu        sync.Mutex
	processes []*exec.Cmd
}

func NewServiceManager(rootPath string) *ServiceManager {
	return &ServiceManager{rootPath: rootPath}
}

var Service []string

func (sm *ServiceManager) startServices(dirName, ext string) error {
	dirPath := filepath.Join(sm.rootPath, dirName)
	dirs, err := os.ReadDir(dirPath)
	if err != nil {
		return err
	}

	for _, entry := range dirs {
		if !entry.IsDir() {
			continue
		}
		if !slices.Contains(Service, entry.Name()) {
			continue
		}

		serviceName := entry.Name()
		dir := filepath.Join(dirPath, serviceName)
		fileName := fmt.Sprintf("%s.%s", serviceName, ext)
		cmd := exec.Command("go", "run", fileName)
		cmd.Dir = dir
		setProcessGroup(cmd)

		logDir := filepath.Join(sm.rootPath, "dev_logs")
		if err := os.MkdirAll(logDir, 0755); err != nil {
			return err
		}

		logFile, err := os.Create(filepath.Join(logDir, fmt.Sprintf("%s.log", serviceName)))
		if err != nil {
			fmt.Printf("Error creating log file for %s: %v\n", serviceName, err)
			continue
		}

		cmd.Stdout = logFile
		cmd.Stderr = logFile
		if err := cmd.Start(); err != nil {
			_ = logFile.Close()
			fmt.Printf("Error running %s %s: %v\n", dirName, serviceName, err)
			continue
		}

		sm.addProcess(cmd)
		go func(name string, cmd *exec.Cmd, logFile *os.File) {
			_ = cmd.Wait()
			_ = logFile.Close()
			sm.removeProcess(cmd)
		}(serviceName, cmd, logFile)

		fmt.Printf("Started %s, logs: dev_logs/%s.log\n", serviceName, serviceName)
	}

	return nil
}

func (sm *ServiceManager) addProcess(cmd *exec.Cmd) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.processes = append(sm.processes, cmd)
}

func (sm *ServiceManager) removeProcess(cmd *exec.Cmd) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	for i, item := range sm.processes {
		if item == cmd {
			sm.processes = append(sm.processes[:i], sm.processes[i+1:]...)
			return
		}
	}
}

func (sm *ServiceManager) stopProcesses() {
	sm.mu.Lock()
	processes := append([]*exec.Cmd(nil), sm.processes...)
	sm.mu.Unlock()

	for _, cmd := range processes {
		killProcessTree(cmd)
	}
}

func (sm *ServiceManager) handleSignals() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	for sig := range sigCh {
		fmt.Printf("Received signal: %s\n", sig)
		sm.stopProcesses()
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

	var services string
	var serviceSet []string
	servicesPath := filepath.Join(root, "services")
	serviceDir, err := os.ReadDir(servicesPath)
	if err != nil {
		panic(err)
	}
	for _, service := range serviceDir {
		if service.IsDir() {
			serviceSet = append(serviceSet, service.Name())
		}
	}

	flag.StringVar(&services, "services", "", fmt.Sprintf("choose services to run: %v", strings.Join(serviceSet, ",")))
	flag.Parse()

	if services != "" {
		Service = strings.Split(services, ",")
		for _, svc := range Service {
			if !slices.Contains(serviceSet, svc) {
				log.Fatalf("Invalid service name: %s. Available services: %v", svc, serviceSet)
			}
		}
	} else {
		Service = serviceSet
	}

	log.Println("you will run services:", Service)
	sm := NewServiceManager(root)
	if err := sm.startServices("services", "go"); err != nil {
		panic(err)
	}
	sm.handleSignals()
}
