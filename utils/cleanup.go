package utils

import (
	"ShimBot-D/config"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	tempCleanupTimer *time.Timer
	cleanupMutex     sync.Mutex
)

func ResetTempCleanupTimer() {
	cleanupMutex.Lock()
	defer cleanupMutex.Unlock()

	// 기존 타이머가 있다면 취소
	if tempCleanupTimer != nil {
		tempCleanupTimer.Stop()
	}

	// 뒤에 삭제 실행
	tempCleanupTimer = time.AfterFunc(time.Duration(config.AppConfig.ExpirySeconds)*time.Second, func() {
		cleanupTempFolder()
	})
}

func cleanupTempFolder() {
	tempPath := config.AppConfig.TempDirectoryName

	// 폴더 내 모든 파일 삭제
	files, err := os.ReadDir(tempPath)
	if err != nil {
		log.Printf("Temp 폴더 읽기 실패: %v", err)
		return
	}

	for _, f := range files {
		os.RemoveAll(filepath.Join(tempPath, f.Name()))
	}
	log.Println("Temp 폴더가 비워졌습니다.")
}
