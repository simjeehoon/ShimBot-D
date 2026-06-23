package config

import (
	"encoding/json"
	"errors" // 에러 생성을 위해 추가
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	Token             string `json:"token"`
	Domain            string `json:"domain"`
	Port              int    `json:"port"`
	TempDirectoryName string `json:"temp_directory_name"`
	ExpirySeconds     int    `json:"expiry_seconds"`
}

var AppConfig Config

func LoadConfig() error {
	file, err := os.Open("config.json")
	if err != nil {
		return err
	}
	defer file.Close()

	err = json.NewDecoder(file).Decode(&AppConfig)
	if err != nil {
		return err
	}

	// 봇 토큰이 없을때
	if AppConfig.Token == "discord_bot_token" || strings.TrimSpace(AppConfig.Token) == "" {
		return errors.New("디스코드 봇 토큰을 입력해주세요")
	}

	// 1. Domain에서 혹시 모를 http://, https:// 제거
	AppConfig.Domain = strings.TrimPrefix(AppConfig.Domain, "http://")
	AppConfig.Domain = strings.TrimPrefix(AppConfig.Domain, "https://")

	// 2. TempDirectoryName에서 순수 폴더명만 떼어내기 (예: "./temp/" -> "temp")
	pureDir := filepath.Base(AppConfig.TempDirectoryName)
	AppConfig.TempDirectoryName = strings.Trim(pureDir, "./ ")

	return nil
}
