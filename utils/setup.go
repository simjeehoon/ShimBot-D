package utils

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// CheckAndSetupEnvironment 함수는 프로그램 시작 시 가상환경을 체크하고 자동으로 구축하거나 업데이트합니다.
func CheckAndSetupEnvironment() error {

	venvDir := ".venv"

	// OS별 가상환경 내부 파이썬 실행 파일 경로 분기 세팅
	var venvPythonPath string
	if runtime.GOOS == "windows" {
		venvPythonPath = filepath.Join(venvDir, "Scripts", "python.exe")
	} else {
		venvPythonPath = filepath.Join(venvDir, "bin", "python")
	}

	// 1. 이미 .venv 폴더가 존재하면 yt-dlp 업데이트만 수행하고 넘어갑니다.
	if _, err := os.Stat(venvDir); !os.IsNotExist(err) {
		log.Println("[Setup] yt-dlp 업데이트 확인 중...")

		// 가상환경 내부의 python -m pip install --upgrade yt-dlp 실행
		updateCmd := exec.Command(venvPythonPath, "-m", "pip", "install", "--upgrade", "yt-dlp")
		updateCmd.Stdout = os.Stdout
		updateCmd.Stderr = os.Stderr

		if err := updateCmd.Run(); err != nil {
			log.Printf("[Setup] yt-dlp 업데이트 실패 (기존 버전으로 계속 진행합니다): %v", err)
		} else {
			log.Println("[Setup] yt-dlp 업데이트가 성공적으로 완료되었습니다.")
		}

		// config.json 체크 단계(6번)로 바로 이동하기 위해 하단 로직을 태우거나,
		// 기존 코드 구조 유지를 위해 아래 config.json 체크 함수를 호출하고 return 합니다.
		return checkConfig()
	}

	log.Println("[Setup] Python .venv 생성 ...")

	// 2. OS별 명령어 및 경로 분기 세팅 (초기 생성용)
	var pythonCmd string
	if runtime.GOOS == "windows" {
		pythonCmd = "python"
	} else {
		pythonCmd = "python3"

		// 리눅스 전용 사전 체크: ffmpeg 설치 여부 확인
		if _, err := exec.LookPath("ffmpeg"); err != nil {
			return fmt.Errorf("시스템에 ffmpeg이 설치되어 있지 않습니다. 'sudo apt install ffmpeg'을 먼저 실행해주세요")
		}
	}

	// 3. 파이썬 가상환경 생성 (python -m venv .venv)
	log.Println("[Setup] 파이썬 가상환경(.venv) 생성 중...")
	createVenvCmd := exec.Command(pythonCmd, "-m", "venv", venvDir)
	createVenvCmd.Stdout = os.Stdout
	createVenvCmd.Stderr = os.Stderr
	if err := createVenvCmd.Run(); err != nil {
		return fmt.Errorf("파이썬 가상환경 생성 실패: %v (파이썬이 설치되어 있고 환경변수에 등록되었는지 확인하세요)", err)
	}

	// 4. pip 업그레이드
	log.Println("[Setup] pip 업그레이드 중...")
	upgradePipCmd := exec.Command(venvPythonPath, "-m", "pip", "install", "--upgrade", "pip")
	upgradePipCmd.Stdout = os.Stdout
	upgradePipCmd.Stderr = os.Stderr
	if err := upgradePipCmd.Run(); err != nil {
		log.Printf("[Setup] pip 업그레이드 실패 (진행은 계속합니다): %v", err)
	}

	// 5. yt-dlp 최초 설치
	log.Println("[Setup] yt-dlp 라이브러리 설치 중...")
	installYtdlCmd := exec.Command(venvPythonPath, "-m", "pip", "install", "yt-dlp")
	installYtdlCmd.Stdout = os.Stdout
	installYtdlCmd.Stderr = os.Stderr
	if err := installYtdlCmd.Run(); err != nil {
		return fmt.Errorf("yt-dlp 설치 실패: %v", err)
	}

	log.Println("[Setup] 모든 환경 세팅이 성공적으로 완료되었습니다!")

	return checkConfig()
}

// 6. config.json 파일 체크 및 복사 로직 (중복 제거를 위해 분리)
func checkConfig() error {
	configFile := "config.json"
	templateFile := "config-template.json"

	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		log.Println("[Setup] config.json 설정 파일이 존재하지 않습니다.")

		if _, tErr := os.Stat(templateFile); tErr == nil {
			log.Println("[Setup] config-template.json 을 기반으로 config.json 을 자동 생성합니다...")

			input, err := os.ReadFile(templateFile)
			if err != nil {
				return fmt.Errorf("템플릿 파일을 읽지 못했습니다: %v", err)
			}

			err = os.WriteFile(configFile, input, 0644)
			if err != nil {
				return fmt.Errorf("config.json 생성에 실패했습니다: %v", err)
			}
			log.Println("[Setup] config.json 생성이 완료되었습니다! 디스코드 토큰(token) 및 도메인을 알맞게 수정 후 재실행해주세요)")
			os.Exit(0)
		} else {
			return fmt.Errorf("설정 파일(config.json)과 템플릿 파일(%s)이 모두 존재하지 않습니다", templateFile)
		}
	}
	return nil
}
