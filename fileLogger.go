// Package: fileLogger
// File: fileLogger.go
// Created by: mint(mint.zhao.chiu@gmail.com)_aiwuTech
// Useage:
// DATE: 14-8-23 17:20
package fileLogger

import (
	"log"
	"os"
	"strconv"
	"sync"
	"time"
)

const (
	DATEFORMAT       = "2006-01-02"
	DEFAULT_LOG_SCAN = 60
)

type UNIT int64

const (
	_       = iota
	KB UNIT = 1 << (iota * 10)
	MB
	GB
	TB
)

type SplitType byte

const (
	SplitType_Size SplitType = iota
	SplitType_Daily
)

type FileLogger struct {
	splitType SplitType
	mu        *sync.RWMutex
	fileDir   string
	fileName  string
	suffix    int
	fileCount int
	fileSize  int64
	prefix    string

	date    *time.Time
	logFile *os.File
	lg      *log.Logger
}

// NewDefaultLogger return a logger split by fileSize by default
func NewDefaultLogger(fileDir, fileName string) *FileLogger {
	return NewSizeLogger(fileDir, fileName, "", 10, 50, MB)
}

// NewSizeLogger return a logger split by fileSize
// Parameters:
// 		file directory
// 		file name
// 		log's prefix
// 		fileCount holds maxCount of bak file
//		fileSize holds each of bak file's size
// 		unit stands for kb, mb, gb, tb
func NewSizeLogger(fileDir, fileName, prefix string, fileCount int, fileSize int64, unit UNIT) *FileLogger {
	sizeLogger := &FileLogger{
		splitType: SplitType_Size,
		mu:        new(sync.RWMutex),
		fileDir:   fileDir,
		fileName:  fileName,
		fileCount: fileCount,
		fileSize:  fileSize * int64(unit),
		prefix:    prefix,
	}

	sizeLogger.initLogger()

	return sizeLogger
}

// NewDailyLogger return a logger split by daily
// Parameters:
// 		file directory
// 		file name
// 		log's prefix
func NewDailyLogger(fileDir, fileName, prefix string) *FileLogger {
	dailyLogger := &FileLogger{
		splitType: SplitType_Daily,
		mu:        new(sync.RWMutex),
		fileDir:   fileDir,
		fileName:  fileName,
		prefix:    prefix,
	}

	dailyLogger.initLogger()

	return dailyLogger
}

func (f *FileLogger) initLogger() {

	switch f.splitType {
	case SplitType_Size:
		f.initLoggerBySize()
	case SplitType_Daily:
		f.initLoggerByDaily()
	}

}

// init filelogger split by fileSize
func (f *FileLogger) initLoggerBySize() {

	f.mu.Lock()
	defer f.mu.Unlock()

	logFile := joinFilePath(f.fileDir, f.fileName)
	for i := 1; i <= f.fileCount; i++ {
		if isExist(logFile + "." + strconv.Itoa(i)) {
			f.suffix = i
		}

		break
	}

	if !f.isMustSplit() {
		if !isExist(f.fileDir) {
			os.Mkdir(f.fileDir, 0755)
		}
		f.logfile, _ = os.OpenFile(logFile, os.O_RDWR|os.O_APPEND|os.O_CREATE, 0666)
		f.lg = log.New(f.logfile, f.prefix, log.LstdFlags|log.Lmicroseconds)
	} else {
		f.split()
	}

	go f.fileMonitor()
}

// init fileLogger split by daily
func (f *FileLogger) initLoggerByDaily() {

	t, _ := time.Parse(DATEFORMAT, time.Now().Format(DATEFORMAT))

	f.date = &t
	f.mu.Lock()
	defer f.mu.Unlock()

	logFile := joinFilePath(f.fileDir, f.fileName)
	if !f.isMustSplit() {
		if !isExist(f.fileDir) {
			os.Mkdir(f.fileDir, 0755)
		}
		f.logfile, _ = os.OpenFile(logFile, os.O_RDWR|os.O_APPEND|os.O_CREATE, 0666)
		f.lg = log.New(f.logfile, f.prefix, log.LstdFlags|log.Lmicroseconds)
	} else {
		f.split()
	}

	go f.fileMonitor()
}

// used for determine the fileLogger f is time to split.
// size: once the current fileLogger's fileSize >= config.fileSize need to split
// daily: once the current fileLogger stands for yesterday need to split
func (f *FileLogger) isMustSplit() bool {

	switch f.splitType {
	case SplitType_Size:
		logFile := joinFilePath(f.fileDir, f.fileName)
		if f.fileCount > 1 {
			if fileSize(logFile) >= f.fileSize {
				return true
			}
		}
	case SplitType_Daily:
		t, _ := time.Parse(DATEFORMAT, time.Now().Format(DATEFORMAT))
		if t.After(*f.date) {
			return true
		}
	}

	return false
}

// Split fileLogger
func (f *FileLogger) split() {

	logFile := joinFilePath(f.fileDir, f.fileName)

	switch f.splitType {
	case SplitType_Size:
		f.suffix = int(f.suffix%f.fileCount + 1)
		if f.logFile != nil {
			f.logFile.Close()
		}

		logFileBak := logFile + "." + strconv.Itoa(f.suffix)
		if isExist(logFileBak) {
			os.Remove(logFileBak)
		}
		os.Rename(logFile, logFileBak)

		f.logfile, _ = os.Create(logFile)
		f.lg = log.New(f.logfile, f.prefix, log.LstdFlags|log.Lmicroseconds)

	case SplitType_Daily:
		logFileBak := logFile + "." + f.date.Format(DATEFORMAT)
		if !isExist(logFileBak) && f.isMustSplit() {
			if f.logFile != nil {
				f.logFile.Close()
			}

			err := os.Rename(logFile, logFileBak)
			if err != nil {
				f.lg.Printf("FileLogger rename error: %v", err.Error())
			}

			t, _ := time.Parse(DATEFORMAT, time.Now().Format(DATEFORMAT))
			f.date = &t
			f.logFile, _ = os.Create(logFile)
			f.lg = log.New(f.logFile, f.prefix, log.LstdFlags|log.Lmicroseconds)
		}
	}
}

// After some interval time, goto check the current fileLogger's size or date
func (f *FileLogger) fileMonitor() {

	//TODO  load logScan interval from config file
	logScan := DEFAULT_LOG_SCAN

	timer := time.NewTicker(time.Duration(logScan) * time.Second)
	for {
		select {
		case <-timer.C:
			f.fileCheck()
		}
	}
}

// If the current fileLogger need to split, just split
func (f *FileLogger) fileCheck() {
	defer func() {
		if err := recover(); err != nil {
			f.lg.Printf("FileLogger catch panic in fileCheck: %v", err.Error())
		}
	}()

	if f.isMustSplit() {
		f.mu.Lock()
		defer f.mu.Unlock()

		f.split()
	}
}
