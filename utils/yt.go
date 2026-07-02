package utils

import (
	"ShimBot-D/config"
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// 동시 다운로드 개수를 제한하기 위한 전역 세마포어 채널 정의
var downloadSemaphore = make(chan struct{}, 3)

// 파이썬 path를 OS별로 반환하는 함수
func getPythonPath() string {
	if runtime.GOOS == "linux" {
		return filepath.Join(".", ".venv", "bin", "python")
	} else {
		return filepath.Join(".", ".venv", "Scripts", "python.exe")
	}
}

// 🔄 재시도/재다운로드 버튼 액션 처리 로직 (실패, 파기, 만료 시 공통 사용)
func HandleYtRetryButton(s *discordgo.Session, i *discordgo.InteractionCreate, customID string) {
	data := strings.TrimPrefix(customID, "yt_retry_")
	parts := strings.SplitN(data, "|", 2)
	if len(parts) < 2 {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "⚠️ 재시도 파싱 데이터 형식이 잘못되었습니다.",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	youtubeURL := parts[0]
	resolution := parts[1]

	userObj := i.User
	if userObj == nil && i.Member != nil {
		userObj = i.Member.User
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredMessageUpdate,
	})

	log.Printf("[yt-download-dm] %s 사용자가 재시도 버튼 클릭 (URL: %s, 화질: %s)", userObj.Username, youtubeURL, resolution)

	loadingText := fmt.Sprintf("🔄 `%s` **%s** 를 다시 다운로드하겠습니다.", youtubeURL, resolution)
	s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Content:    &loadingText,
		Components: &[]discordgo.MessageComponent{},
	})

	mockMessage := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			ChannelID: i.ChannelID,
			Author:    userObj,
		},
	}

	go ProcessYoutubeDownloadForMessage(s, mockMessage, youtubeURL, resolution)
}

func ProcessYoutubeDownloadForMessage(s *discordgo.Session, m *discordgo.MessageCreate, youtubeURL string, resolution string) {
	if strings.TrimSpace(resolution) == "" {
		resolution = "720p"
	}
	extension := "mp4"
	if resolution == "mp3" {
		extension = "mp3"
	}

	statusMsg, err := s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("▶️ `%s` (**%s**) 다운로드 요청 접수됨", youtubeURL, resolution))
	if err != nil {
		log.Printf("[yt-download-dm] ⚠️ 초기 메시지 전송 실패: %v", err)
		return
	}

	select {
	case downloadSemaphore <- struct{}{}:
	default:
		s.ChannelMessageEdit(m.ChannelID, statusMsg.ID, fmt.Sprintf("⏳ `%s` (**%s**) **대기 중...**", youtubeURL, resolution))
		downloadSemaphore <- struct{}{}
	}

	defer func() { <-downloadSemaphore }()

	firstFileName := fmt.Sprintf("ytdl_%d_%s_%s.%s", time.Now().Unix(), m.Author.ID, randomHex(2), extension)
	filePath := filepath.Join(config.AppConfig.TempDirectoryName, firstFileName)

	cmd := exec.Command(getPythonPath(), "yt-downloader.py", youtubeURL, filePath, resolution, "--recode-video", "mp4")
	cmd.Env = append(os.Environ(), "PYTHONIOENCODING=utf-8", "PATH="+os.Getenv("PATH"))

	log.Printf("[yt-download-dm] %s 다운로드 시작 (기본파일명 후보: %s)", youtubeURL, firstFileName)

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		s.ChannelMessageEdit(m.ChannelID, statusMsg.ID, "❌ 오류 발생: 파이썬 파이프 생성 실패")
		return
	}
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf

	if err := cmd.Start(); err != nil {
		s.ChannelMessageEdit(m.ChannelID, statusMsg.ID, "❌ 오류 발생: 파이썬 프로세스 시작 실패")
		return
	}

	s.ChannelMessageEdit(m.ChannelID, statusMsg.ID, fmt.Sprintf("🔬 **`%s` (**%s**) 다운로드 준비 중...**", youtubeURL, resolution))

	var meta VideoMeta
	var pythonError string
	var finalFileName string
	isPlaylist := false

	lastProgressUpdate := time.Now()
	var fullLogBuf bytes.Buffer

	scanner := bufio.NewScanner(stdoutPipe)
	for scanner.Scan() {
		line := scanner.Text()
		fullLogBuf.WriteString(line)
		fullLogBuf.WriteString("\n")
		text := strings.TrimSpace(line)
		if text == "" {
			continue
		}

		if strings.HasPrefix(text, "METADATA:") {
			jsonData := strings.TrimPrefix(text, "METADATA:")
			_ = json.Unmarshal([]byte(jsonData), &meta)
			continue
		}

		if strings.HasPrefix(text, "PROGRESS:") {
			percent := strings.TrimPrefix(text, "PROGRESS:")
			if time.Since(lastProgressUpdate) > 1500*time.Millisecond || percent == "100.0" {
				displayTitle := youtubeURL
				if meta.Title != "" {
					displayTitle = meta.Title
				}
				s.ChannelMessageEdit(m.ChannelID, statusMsg.ID, fmt.Sprintf("📥 **`%s` (**%s**) 다운로드 중... (%s%%)**", displayTitle, resolution, percent))
				lastProgressUpdate = time.Now()
			}
			continue
		}

		if strings.HasPrefix(text, "ERROR:") || strings.Contains(strings.ToLower(text), "error") {
			pythonError = strings.TrimPrefix(text, "ERROR:")
			if pythonError == "" {
				pythonError = text
			}
		}

		if strings.HasPrefix(text, "PLAYLIST:") {
			isPlaylist = true
		}

		if strings.HasPrefix(text, "FILENAME:") {
			finalFileName = strings.TrimPrefix(text, "FILENAME:")
		}
	}

	if err := scanner.Err(); err != nil {
		log.Printf("[yt-download-dm] stdout scanner error: %v", err)
		if pythonError == "" {
			pythonError = err.Error()
		}
	}

	_ = cmd.Wait()
	if errBuf.Len() > 0 {
		log.Printf("[Python Stderr Output]\n%s", errBuf.String())
	}

	if isPlaylist || pythonError != "" || finalFileName == "" {
		cleanUpGarbageFiles(config.AppConfig.TempDirectoryName, firstFileName)

		var errMsg string
		if isPlaylist {
			errMsg = "▶️ 플레이리스트는 다운로드하지 못합니다. 개별 영상 주소를 입력해주세요."
		} else if pythonError != "" {
			errMsg = fmt.Sprintf("😭 원인: %s", pythonError)
		} else {
			errMsg = "😭 다운로드 프로세스가 비정상적으로 종료되었거나 파일명을 반환받지 못했습니다."
		}

		errText := fmt.Sprintf("❌ **`%s` (**%s**) 다운로드 실패**\n%s", youtubeURL, resolution, errMsg)
		retryID := fmt.Sprintf("yt_retry_%s|%s", youtubeURL, resolution)

		actionsRow := discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Label:    "🔄 재시도",
					Style:    discordgo.PrimaryButton,
					CustomID: retryID,
				},
			},
		}

		s.ChannelMessageDelete(m.ChannelID, statusMsg.ID)
		_, err = s.ChannelMessageSendComplex(m.ChannelID, &discordgo.MessageSend{
			Content:    errText,
			Components: []discordgo.MessageComponent{actionsRow},
		})
		return
	}

	realFilePath := filepath.Join(config.AppConfig.TempDirectoryName, finalFileName)
	expireDuration := time.Duration(config.AppConfig.ExpirySeconds) * time.Second
	expireTime := time.Now().Add(expireDuration)
	expireTimeStr := expireTime.Format("2006년 01월 02일 15:04")

	tracker := &FileTracker{
		filePath:      realFilePath,
		videoTitle:    meta.Title,
		isExpired:     false,
		channelID:     m.ChannelID,
		resolution:    resolution,
		expireTime:    expireTimeStr,
		thumbnail:     meta.Thumbnail,
		youtubeURL:    youtubeURL,
		downloadCount: 0,
	}

	mapMutex.Lock()
	trackMap[finalFileName] = tracker
	mapMutex.Unlock()

	downloadURL := fmt.Sprintf("http://%s:%d/%s/%s", config.AppConfig.Domain, config.AppConfig.Port, config.AppConfig.TempDirectoryName, finalFileName)

	embed := &discordgo.MessageEmbed{
		Title:       "**☑️ 다운로드 준비 완료**",
		Description: fmt.Sprintf("**%s** (**%s**)\n\n[다운로드(클릭)](%s)", meta.Title, resolution, downloadURL),
		Color:       0xff0000,
		Fields: []*discordgo.MessageEmbedField{
			{Name: "🎥 화질/포맷", Value: fmt.Sprintf("`%s`", resolution), Inline: true},
			{Name: "⏱️ 만료 시간", Value: fmt.Sprintf("`%s에 만료`", expireTimeStr), Inline: true},
		},
	}
	if meta.Thumbnail != "" {
		embed.Image = &discordgo.MessageEmbedImage{URL: meta.Thumbnail}
	}

	actionsRow := discordgo.ActionsRow{
		Components: []discordgo.MessageComponent{
			discordgo.Button{
				Label:    "🗑️ 제거",
				Style:    discordgo.DangerButton,
				CustomID: "delete_file_" + finalFileName,
			},
		},
	}

	s.ChannelMessageDelete(m.ChannelID, statusMsg.ID)
	finalMsg, err := s.ChannelMessageSendComplex(m.ChannelID, &discordgo.MessageSend{
		Embeds:     []*discordgo.MessageEmbed{embed},
		Components: []discordgo.MessageComponent{actionsRow},
	})
	if err != nil {
		log.Printf("[yt-download-dm] ⚠️ 최종 메시지 전송 실패: %v", err)
	} else {
		mapMutex.Lock()
		tracker.messageID = finalMsg.ID
		mapMutex.Unlock()
	}

	ResetTempCleanupTimer()

	time.AfterFunc(expireDuration, func() {
		mapMutex.Lock()
		if tracker.isExpired {
			mapMutex.Unlock()
			return
		}
		tracker.isExpired = true
		delete(trackMap, finalFileName)

		// 💡 락이 쥐어진 상태에서 다운로드 횟수 확인 및 저장
		hasDownloaded := tracker.downloadCount > 0
		mapMutex.Unlock()

		log.Printf("[yt-download-dm] %s 만료 시간에 도달하여 파일 자동 파기 시작", finalFileName)
		go func() {
			tracker.wg.Wait()
			if _, err := os.Stat(realFilePath); err == nil {
				_ = os.Remove(realFilePath)
			}

			var expiredEmbed *discordgo.MessageEmbed

			// 💡 다운로드 이력이 있는 경우와 없는 경우 임베드 분기
			if hasDownloaded {
				expiredEmbed = &discordgo.MessageEmbed{
					Title:       "**✅ 다운로드 완료**", // 연두색 타이틀 유지
					Description: fmt.Sprintf("**%s**\n`%s`\n~~[링크 만료됨]~~ ", meta.Title, tracker.youtubeURL),
					Color:       0x00FF00, // 연두색 유지
					Fields: []*discordgo.MessageEmbedField{
						{Name: "🎥 화질/포맷", Value: fmt.Sprintf("`%s`", tracker.resolution), Inline: true},
						{Name: "⏱️ 만료 시간", Value: fmt.Sprintf("`%s (만료됨)`", tracker.expireTime), Inline: true},
					},
				}
			} else {
				// 다운로드 이력이 전혀 없는 경우 (기존 회색 만료 임베드)
				expiredEmbed = &discordgo.MessageEmbed{
					Title:       "🛑 링크 만료됨",
					Description: fmt.Sprintf("~~%s~~\n`%s`\n화질/포맷: `%s`", meta.Title, tracker.youtubeURL, tracker.resolution),
					Color:       0x404040,
				}
			}

			if meta.Thumbnail != "" && hasDownloaded {
				expiredEmbed.Image = &discordgo.MessageEmbedImage{URL: meta.Thumbnail}
			}

			retryID := fmt.Sprintf("yt_retry_%s|%s", tracker.youtubeURL, tracker.resolution)
			retryRow := discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					discordgo.Button{
						Label:    "🔄 다시 다운로드",
						Style:    discordgo.PrimaryButton,
						CustomID: retryID,
					},
				},
			}

			_, err = s.ChannelMessageEditComplex(&discordgo.MessageEdit{
				ID:         finalMsg.ID,
				Channel:    m.ChannelID,
				Embeds:     &[]*discordgo.MessageEmbed{expiredEmbed},
				Components: &[]discordgo.MessageComponent{retryRow}, // 버튼 업데이트
			})
		}()
	})
}

func HandleYtDeleteButton(s *discordgo.Session, i *discordgo.InteractionCreate, customID string) {
	fileName := strings.TrimPrefix(customID, "delete_file_")

	mapMutex.Lock()
	tracker, exists := trackMap[fileName]
	if !exists || tracker.isExpired {
		mapMutex.Unlock()
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "⚠️ 이미 만료되었거나 삭제된 파일입니다.",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}
	tracker.isExpired = true
	delete(trackMap, fileName)
	mapMutex.Unlock()

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredMessageUpdate,
	})

	go func() {
		tracker.wg.Wait()
		_ = os.Remove(tracker.filePath)

		deletedEmbed := &discordgo.MessageEmbed{
			Title:       "제거됨",
			Description: fmt.Sprintf("~~%s~~\n`%s`\n화질/포맷: `%s`", tracker.videoTitle, tracker.youtubeURL, tracker.resolution),
			Color:       0x404040,
		}

		retryID := fmt.Sprintf("yt_retry_%s|%s", tracker.youtubeURL, tracker.resolution)
		retryRow := discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Label:    "🔄 다시 다운로드",
					Style:    discordgo.PrimaryButton,
					CustomID: retryID,
				},
			},
		}

		if tracker.channelID != "" && tracker.messageID != "" {
			_, err := s.ChannelMessageEditComplex(&discordgo.MessageEdit{
				ID:         tracker.messageID,
				Channel:    tracker.channelID,
				Embeds:     &[]*discordgo.MessageEmbed{deletedEmbed},
				Components: &[]discordgo.MessageComponent{retryRow}, // 버튼 업데이트
			})
			if err != nil {
				log.Printf("[yt-download] ⚠️ 삭제 임베드 업데이트 실패: %v", err)
			}
		}
	}()
}

func StartFileServer(s *discordgo.Session) {
	serverOnce.Do(func() {
		if _, err := os.Stat(config.AppConfig.TempDirectoryName); !os.IsNotExist(err) {
			_ = os.RemoveAll(config.AppConfig.TempDirectoryName)
		}
		_ = os.MkdirAll(config.AppConfig.TempDirectoryName, os.ModePerm)

		trackMap = make(map[string]*FileTracker)

		http.HandleFunc(
			fmt.Sprintf("/%s/", config.AppConfig.TempDirectoryName),
			func(w http.ResponseWriter, r *http.Request) {
				fileName := filepath.Base(r.URL.Path)

				mapMutex.Lock()
				tracker, exists := trackMap[fileName]
				if !exists || tracker.isExpired {
					mapMutex.Unlock()
					http.Error(w, "만료되거나 파기된 링크입니다.", http.StatusNotFound)
					return
				}

				tracker.downloadCount++
				currentCount := tracker.downloadCount

				fileExt := filepath.Ext(fileName)
				downloadName := getVideoFileName(fileName, tracker.videoTitle, fileExt)

				tracker.wg.Add(1)
				mapMutex.Unlock()

				defer tracker.wg.Done()

				filePath := filepath.Join(config.AppConfig.TempDirectoryName, fileName)
				encodedName := strings.ReplaceAll(url.QueryEscape(downloadName), "+", "%20")

				w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename*=UTF-8''%s", encodedName))
				w.Header().Set("Content-Type", "application/octet-stream")

				http.ServeFile(w, r, filePath)

				if tracker != nil {
					downloadURL := fmt.Sprintf("http://%s:%d/%s/%s", config.AppConfig.Domain, config.AppConfig.Port, config.AppConfig.TempDirectoryName, fileName)

					completeEmbed := &discordgo.MessageEmbed{
						Title:       "**✅ 다운로드 완료**",
						Description: fmt.Sprintf("**%s**\n\n[재다운로드(클릭)](%s) (%d회 다운로드함)", tracker.videoTitle, downloadURL, currentCount),
						Color:       0x00FF00,
						Fields: []*discordgo.MessageEmbedField{
							{Name: "🎥 화질/포맷", Value: fmt.Sprintf("`%s`", tracker.resolution), Inline: true},
							{Name: "⏱️ 만료 시간", Value: fmt.Sprintf("`%s`", tracker.expireTime), Inline: true},
						},
					}

					if tracker.thumbnail != "" {
						completeEmbed.Image = &discordgo.MessageEmbedImage{URL: tracker.thumbnail}
					}

					actionsRow := discordgo.ActionsRow{
						Components: []discordgo.MessageComponent{
							discordgo.Button{
								Label:    "🗑️ 제거",
								Style:    discordgo.DangerButton,
								CustomID: "delete_file_" + fileName,
							},
						},
					}

					if tracker.channelID != "" && tracker.messageID != "" {
						_, err := s.ChannelMessageEditComplex(&discordgo.MessageEdit{
							ID:         tracker.messageID,
							Channel:    tracker.channelID,
							Embeds:     &[]*discordgo.MessageEmbed{completeEmbed},
							Components: &[]discordgo.MessageComponent{actionsRow},
						})
						if err != nil {
							log.Printf("[yt-download] ⚠️ 다운로드 완료 임베드 갱신 실패: %v", err)
						}
					}
				}
			})

		go func() {
			_ = http.ListenAndServe(":"+fmt.Sprint(config.AppConfig.Port), nil)
		}()
	})
}

func cleanUpGarbageFiles(dirPath string, baseFileName string) {
	ext := filepath.Ext(baseFileName)
	prefix := strings.TrimSuffix(baseFileName, ext)

	files, err := os.ReadDir(dirPath)
	if err != nil {
		return
	}

	for _, file := range files {
		if file.IsDir() {
			continue
		}
		name := file.Name()

		if strings.HasPrefix(name, prefix) {
			if name != prefix+".mp4" && name != prefix+".mp3" {
				_ = os.Remove(filepath.Join(dirPath, name))
			}
		}
	}
}

type VideoMeta struct {
	Title     string `json:"title"`
	Thumbnail string `json:"thumbnail"`
}

type FileTracker struct {
	filePath      string
	videoTitle    string
	isExpired     bool
	wg            sync.WaitGroup
	channelID     string
	messageID     string
	resolution    string
	expireTime    string
	thumbnail     string
	youtubeURL    string
	downloadCount int
}

var (
	trackMap   map[string]*FileTracker
	mapMutex   sync.Mutex
	serverOnce sync.Once
)

func randomHex(n int) string {
	bytes := make([]byte, n)
	if _, err := rand.Read(bytes); err != nil {
		return "abcd"
	}
	return hex.EncodeToString(bytes)
}

func getClientIP(r *http.Request) string {
	xForwardedFor := r.Header.Get("X-Forwarded-For")
	if xForwardedFor != "" {
		ips := strings.Split(xForwardedFor, ",")
		return strings.TrimSpace(ips[0])
	}

	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

func getVideoFileName(realFileName string, videoTitle string, extension string) string {
	/*
		videoTitle = strings.ReplaceAll(videoTitle, "|", "-")
		videoTitle = strings.ReplaceAll(videoTitle, "/", "-")

		reg := regexp.MustCompile(`[^a-zA-Z0-9가-힣ㄱ-ㅎㅏ-ㅣ\s\-_.]`)
		cleaned := reg.ReplaceAllString(videoTitle, "")

		spaceReg := regexp.MustCompile(`\s+`)
		cleaned = spaceReg.ReplaceAllString(cleaned, " ")
		cleaned = strings.TrimSpace(cleaned)
		cleaned = strings.TrimSuffix(cleaned, ".")
		cleaned = strings.TrimSpace(cleaned)

		if cleaned == "" {
			return realFileName
		} else {
			return cleaned + extension
		}
	*/
	return videoTitle + extension
}
