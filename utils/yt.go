package utils

import (
	"ShimBot-D/config"
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
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// 💡 1. 동시 다운로드 개수를 5개로 제한하기 위한 전역 세마포어 채널 정의
var downloadSemaphore = make(chan struct{}, 3)

// 1. 명령어 정의 데이터
var YtCommand = &discordgo.ApplicationCommand{
	Name:        "yt",
	Description: "유튜브 영상을 다운로드합니다.",
	Options: []*discordgo.ApplicationCommandOption{
		{
			Type:        discordgo.ApplicationCommandOptionString,
			Name:        "url",
			Description: "유튜브 주소",
			Required:    true,
		},
		{
			Type:        discordgo.ApplicationCommandOptionString,
			Name:        "quality",
			Description: "영상 화질/MP3 (기본값: 720p)",
			Required:    false,
			Choices: []*discordgo.ApplicationCommandOptionChoice{
				{Name: "1080p", Value: "1080p"},
				{Name: "720p", Value: "720p"},
				{Name: "480p", Value: "480p"},
				{Name: "MP3 (최고음질)", Value: "mp3"},
			},
		},
		{
			Type:        discordgo.ApplicationCommandOptionString,
			Name:        "shared",
			Description: "공유 여부 (기본값: 비공개)",
			Required:    false,
			Choices: []*discordgo.ApplicationCommandOptionChoice{
				{Name: "공유", Value: "public"},
				{Name: "비공개", Value: "ephemeral"},
			},
		},
	},
}

// 2. [인자 추출 부] 슬래시 명령어 라우팅 처리
func HandleYt(s *discordgo.Session, i *discordgo.InteractionCreate) {
	options := i.ApplicationCommandData().Options
	var youtubeURL string
	resolution := "720p"
	msgLocation := "ephemeral"

	for _, opt := range options {
		switch opt.Name {
		case "url":
			youtubeURL = opt.StringValue()
		case "quality":
			resolution = opt.StringValue()
		case "shared":
			msgLocation = opt.StringValue()
		}
	}

	userObj := i.User
	if userObj == nil && i.Member != nil {
		userObj = i.Member.User
	}

	var responseFlags discordgo.MessageFlags
	if msgLocation == "ephemeral" {
		responseFlags = discordgo.MessageFlagsEphemeral
	}

	// 먼저 봇이 생각 중임을 응답 (Interaction 전용)
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags: responseFlags,
		},
	})

	// 분리된 핵심 비즈니스 로직 함수 호출
	go ProcessYoutubeDownload(s, i.Interaction, userObj, youtubeURL, resolution)
}

func ProcessYoutubeDownload(s *discordgo.Session, interaction *discordgo.Interaction, userObj *discordgo.User, youtubeURL string, resolution string) {
	// 💡 2. 다운로드 슬롯이 가득 찼을 때 안내 메시지 전송 및 대기 상태 진입
	select {
	case downloadSemaphore <- struct{}{}:
		// 슬롯 여유 있음: 즉시 진행
	default:
		// 슬롯이 꽉 찬 경우 대기 안내를 먼저 보내고 슬롯이 빌 때까지 대기
		waitingText := "⏳ **대기 중...**"
		s.InteractionResponseEdit(interaction, &discordgo.WebhookEdit{Content: &waitingText})
		downloadSemaphore <- struct{}{} // 빈 슬롯이 생길 때까지 대기 블로킹
	}

	// 함수가 종료될 때 반드시 세마포어 슬롯을 반환하여 다음 대기 작업이 수행되도록 함
	defer func() { <-downloadSemaphore }()

	extension := "mp4"
	if resolution == "mp3" {
		extension = "mp3"
	}

	firstFileName := fmt.Sprintf("ytdl_%d_%s_%s.%s", time.Now().Unix(), userObj.ID, randomHex(2), extension)
	filePath := filepath.Join(config.AppConfig.TempDirectoryName, firstFileName)

	var pythonPath string
	if runtime.GOOS == "linux" {
		pythonPath = filepath.Join(".", ".venv", "bin", "python")
	} else {
		pythonPath = filepath.Join(".", ".venv", "Scripts", "python.exe")
	}
	cmd := exec.Command(pythonPath, "yt-downloader.py", youtubeURL, filePath, resolution, "--recode-video", "mp4")
	cmd.Env = append(os.Environ(), "PYTHONIOENCODING=utf-8")

	log.Printf("[yt-download] %s 다운로드 시작 (기본파일명 후보: %s)", youtubeURL, firstFileName)

	var outBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &outBuf

	if err := cmd.Start(); err != nil {
		log.Printf("[yt-download] ⚠️ 프로세스 시작 실패: %v", err)
		sendError(s, interaction, "파이썬 프로세스 시작 실패", youtubeURL, resolution)
		return
	}

	progressText := "⏳ **유튜브 영상을 다운로드 중입니다...**\n영상을 분석하고 다운로드를 시도하고 있습니다. 잠시만 기다려주세요..."
	s.InteractionResponseEdit(interaction, &discordgo.WebhookEdit{Content: &progressText})

	waitErr := cmd.Wait()

	outputStr := outBuf.String()
	log.Printf("[Python Full Output]\n%s", outputStr)

	var meta VideoMeta
	var pythonError string
	var finalFileName string
	isPlaylist := false

	lines := strings.Split(outputStr, "\n")
	for _, line := range lines {
		text := strings.TrimSpace(line)
		if text == "" {
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
		if strings.HasPrefix(text, "SUCCESS:") {
			finalFileName = strings.TrimPrefix(text, "SUCCESS:")
		}
		if strings.HasPrefix(text, "METADATA:") {
			jsonData := strings.TrimPrefix(text, "METADATA:")
			_ = json.Unmarshal([]byte(jsonData), &meta)
		}
	}

	log.Printf("[yt-download] 파이썬 프로세스 종료 상태 (WaitErr: %v, PythonErr: %s, FileName: %s)", waitErr, pythonError, finalFileName)

	if isPlaylist {
		sendError(s, interaction, "▶️ 플레이리스트는 다운로드하지 못합니다.\n개별 영상 주소를 입력해주세요.", youtubeURL, resolution)
		return
	}
	if pythonError != "" {
		cleanUpGarbageFiles(config.AppConfig.TempDirectoryName, firstFileName)
		sendError(s, interaction, fmt.Sprintf("😭 원인: %s", pythonError), youtubeURL, resolution)
		return
	}
	if waitErr != nil || finalFileName == "" {
		cleanUpGarbageFiles(config.AppConfig.TempDirectoryName, firstFileName)
		sendError(s, interaction, fmt.Sprintf("😭 다운로드 실패 (Exit Code 혹은 스크립트 오류)\nWaitErr: %v", waitErr), youtubeURL, resolution)
		return
	}

	realFilePath := filepath.Join(config.AppConfig.TempDirectoryName, finalFileName)
	expireDuration := time.Duration(config.AppConfig.ExpirySeconds) * time.Second
	expireTime := time.Now().Add(expireDuration)
	expireTimeStr := expireTime.Format("2006년 01월 02일 15:04")

	tracker := &FileTracker{
		filePath:    realFilePath,
		videoTitle:  meta.Title,
		isExpired:   false,
		interaction: interaction,
		resolution:  resolution,
		expireTime:  expireTimeStr,
		thumbnail:   meta.Thumbnail,
	}

	mapMutex.Lock()
	trackMap[finalFileName] = tracker
	mapMutex.Unlock()

	downloadURL := fmt.Sprintf("http://%s:%d/%s/%s", config.AppConfig.Domain, config.AppConfig.Port, config.AppConfig.TempDirectoryName, finalFileName)

	embed := &discordgo.MessageEmbed{
		Title:       "**☑️ 다운로드 준비 완료**",
		Description: fmt.Sprintf("**%s**\n\n[다운로드](%s)\n\n", meta.Title, downloadURL),
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
				Label:    "🗑️ 삭제",
				Style:    discordgo.DangerButton,
				CustomID: "delete_file_" + finalFileName,
			},
		},
	}

	emptyStr := ""
	_, err := s.InteractionResponseEdit(interaction, &discordgo.WebhookEdit{
		Content:    &emptyStr,
		Embeds:     &[]*discordgo.MessageEmbed{embed},
		Components: &[]discordgo.MessageComponent{actionsRow},
	})
	if err != nil {
		log.Printf("[yt-download] ⚠️ 최종 메시지 전송 실패: %v", err)
	}

	time.AfterFunc(expireDuration, func() {
		mapMutex.Lock()
		if tracker.isExpired {
			mapMutex.Unlock()
			return
		}
		tracker.isExpired = true
		delete(trackMap, finalFileName)
		mapMutex.Unlock()

		log.Printf("[yt-download] %s 만료 타이머 작동", finalFileName)
		go func() {
			tracker.wg.Wait()
			// 이미 다운로드 완료로 인해 파일이 지워졌을 수 있으므로 확인 후 삭제
			if _, err := os.Stat(realFilePath); err == nil {
				if err := os.Remove(realFilePath); err != nil {
					log.Printf("[yt-download] ⚠️ 파일 자동 파기 실패 (%s): %v", finalFileName, err)
				} else {
					log.Printf("[yt-download] %s 파일 자동 파기 완료", finalFileName)
				}
			}

			expiredEmbed := &discordgo.MessageEmbed{
				Title:       "링크 만료됨",
				Description: fmt.Sprintf("~~%s~~\n\n다운로드 기간이 만료되었습니다.", meta.Title),
				Color:       0x404040,
			}

			// 슬래시 명령어 인터랙션 응답 카드 업데이트 및 버튼 제거
			s.InteractionResponseEdit(interaction, &discordgo.WebhookEdit{
				Embeds:     &[]*discordgo.MessageEmbed{expiredEmbed},
				Components: &[]discordgo.MessageComponent{}, // 💡 버튼 영구 삭제
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
	log.Printf("[yt-download] %s 사용자가 삭제 버튼 클릭", fileName)

	go func() {
		tracker.wg.Wait()
		if err := os.Remove(tracker.filePath); err != nil {
			log.Printf("[yt-download] ⚠️ 파일 자동 파기 실패 (%s): %v", tracker.filePath, err)
		} else {
			log.Printf("[yt-download] %s 파일 자동 파기 완료", tracker.filePath)
		}

		deletedEmbed := &discordgo.MessageEmbed{
			Title:       "🗑️ 사용자에 의해 삭제됨",
			Description: fmt.Sprintf("~~%s~~\n\n삭제되었습니다.", tracker.videoTitle),
			Color:       0x404040,
		}

		// 💡 [교정 포인트] Components에 빈 배열을 주입하여 컴포넌트(버튼) 영역을 완전히 초기화합니다.
		if tracker.interaction != nil {
			_, err := s.InteractionResponseEdit(tracker.interaction, &discordgo.WebhookEdit{
				Embeds:     &[]*discordgo.MessageEmbed{deletedEmbed},
				Components: &[]discordgo.MessageComponent{}, // 🗑️ 삭제 버튼 삭제
			})
			if err != nil {
				log.Printf("[yt-download] ⚠️ 삭제 임베드 업데이트 실패(Interaction): %v", err)
			}
		} else if tracker.channelID != "" && tracker.messageID != "" {
			// 💡 [교정 포인트] ChannelMessageEditEmbed 대신 Complex 함수를 사용하여 Components를 초기화해야 버튼이 지워집니다.
			_, err := s.ChannelMessageEditComplex(&discordgo.MessageEdit{
				ID:         tracker.messageID,
				Channel:    tracker.channelID,
				Embeds:     &[]*discordgo.MessageEmbed{deletedEmbed},
				Components: &[]discordgo.MessageComponent{}, // 🗑️ 삭제 버튼 삭제
			})
			if err != nil {
				log.Printf("[yt-download] ⚠️ 삭제 임베드 업데이트 실패(Message): %v", err)
			}
		}
	}()
}

// 🔄 5. 재시도 버튼 액션 처리 로직
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

	log.Printf("[yt-download] %s 사용자가 재시도 버튼 클릭 (URL: %s, 화질: %s)", userObj.Username, youtubeURL, resolution)

	loadingText := "🔄 사용자의 요청으로 다시 다운로드를 시도하고 있습니다..."
	s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Content:    &loadingText,
		Components: &[]discordgo.MessageComponent{},
	})

	go ProcessYoutubeDownload(s, i.Interaction, userObj, youtubeURL, resolution)
}

func sendError(s *discordgo.Session, interaction *discordgo.Interaction, msg string, youtubeURL string, resolution string) {
	errText := fmt.Sprintf("❌ **`%s` 다운로드 실패**\n%s", youtubeURL, msg)
	log.Printf("[yt-download] 오류 처리 실행: %s", msg)

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

	_, err := s.InteractionResponseEdit(interaction, &discordgo.WebhookEdit{
		Content: &errText,
		Embeds:  &[]*discordgo.MessageEmbed{},
	})

	_, err = s.FollowupMessageCreate(interaction, true, &discordgo.WebhookParams{
		Content:    errText,
		Components: []discordgo.MessageComponent{actionsRow},
	})

	if err != nil {
		log.Printf("[yt-download] ⚠️ 에러 발생 후 재시도 버튼 전송 실패: %v", err)
	}
}

type VideoMeta struct {
	Title     string `json:"title"`
	Thumbnail string `json:"thumbnail"`
}

type FileTracker struct {
	filePath    string
	videoTitle  string
	isExpired   bool
	wg          sync.WaitGroup
	interaction *discordgo.Interaction
	channelID   string
	messageID   string
	resolution  string
	expireTime  string
	thumbnail   string
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
}

func StartFileServer(s *discordgo.Session) {
	serverOnce.Do(func() {
		if _, err := os.Stat(config.AppConfig.TempDirectoryName); !os.IsNotExist(err) {
			if err := os.RemoveAll(config.AppConfig.TempDirectoryName); err != nil {
				log.Printf("[yt-download] 프로그램을 시작하며 temp 디렉토리 삭제 실패: %v", err)
			} else {
				log.Printf("[yt-download] 프로그램을 시작하며 temp 디렉토리 초기화 완료")
			}
		}

		if err := os.MkdirAll(config.AppConfig.TempDirectoryName, os.ModePerm); err != nil {
			log.Printf("[yt-download] temp 디렉토리 생성 실패: %v", err)
		}

		trackMap = make(map[string]*FileTracker)

		http.HandleFunc(
			fmt.Sprintf("/%s/", config.AppConfig.TempDirectoryName),
			func(w http.ResponseWriter, r *http.Request) {
				fileName := filepath.Base(r.URL.Path)

				mapMutex.Lock()
				tracker, exists := trackMap[fileName]
				if !exists || tracker.isExpired {
					mapMutex.Unlock()
					http.Error(w, "만료되거나 삭제된 링크입니다.", http.StatusNotFound)
					return
				}

				fileExt := filepath.Ext(fileName)
				downloadName := getVideoFileName(fileName, tracker.videoTitle, fileExt)

				tracker.wg.Add(1)
				mapMutex.Unlock()

				defer tracker.wg.Done()

				clientIP := getClientIP(r)
				log.Printf("[yt-download] 사용자 파일 전송을 요청 (filename: %s) (IP: %s)", fileName, clientIP)

				filePath := filepath.Join(config.AppConfig.TempDirectoryName, fileName)

				encodedName := url.QueryEscape(downloadName)
				encodedName = strings.ReplaceAll(encodedName, "+", "%20")

				w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename*=UTF-8''%s", encodedName))
				w.Header().Set("Content-Type", "application/octet-stream")

				// 1. 사용자에게 파일을 완전히 전송합니다.
				http.ServeFile(w, r, filePath)
				log.Printf("[yt-download] 사용자 파일 수신 끝 (filename: %s) (IP: %s)", fileName, clientIP)

				// 2. 💡 [핵심] 다운로드가 끝났으므로 전역 맵에서 상태를 즉시 만료 처리하고 삭제합니다.
				mapMutex.Lock()
				if !tracker.isExpired {
					tracker.isExpired = true
					delete(trackMap, fileName)
				}
				mapMutex.Unlock()

				// 3. 💡 [핵심] 로컬 temp 폴더에서 실제 다운로드된 원본 파일을 즉시 완전히 파기합니다.
				if err := os.Remove(filePath); err != nil {
					log.Printf("[yt-download] ⚠️ 다운로드 완료 후 파일 즉시 삭제 실패 (%s): %v", fileName, err)
				} else {
					log.Printf("[yt-download] 🔥 다운로드 완료 후 temp 파일 파기 성공: %s", fileName)
				}

				// 4. 💡 [핵심] 디스코드 임베드 카드 업데이트 (링크 및 버튼 완벽 삭제)
				if tracker != nil {
					completeEmbed := &discordgo.MessageEmbed{
						Title:       "**✅ 다운로드 완료**",
						Description: fmt.Sprintf("**%s**\n\n", tracker.videoTitle),
						Color:       0x00FF00,
					}

					if tracker.thumbnail != "" {
						completeEmbed.Image = &discordgo.MessageEmbedImage{URL: tracker.thumbnail}
					}

					// Components에 빈 배열(&[]discordgo.MessageComponent{})을 전달하여 🗑️ 삭제 버튼 인프라를 삭제합니다.
					if tracker.interaction != nil {
						_, err := s.InteractionResponseEdit(tracker.interaction, &discordgo.WebhookEdit{
							Embeds:     &[]*discordgo.MessageEmbed{completeEmbed},
							Components: &[]discordgo.MessageComponent{}, // 버튼 영구 제거
						})
						if err != nil {
							log.Printf("[yt-download] ⚠️ 수신 완료 임베드 업데이트 실패(Interaction): %v", err)
						}
					} else if tracker.channelID != "" && tracker.messageID != "" {
						_, err := s.ChannelMessageEditComplex(&discordgo.MessageEdit{
							ID:         tracker.messageID,
							Channel:    tracker.channelID,
							Embeds:     &[]*discordgo.MessageEmbed{completeEmbed},
							Components: &[]discordgo.MessageComponent{}, // 버튼 영구 제거
						})
						if err != nil {
							log.Printf("[yt-download] ⚠️ 수신 완료 임베드 업데이트 실패(Message): %v", err)
						}
					}
				}
			})

		go func() {
			_ = http.ListenAndServe(":"+fmt.Sprint(config.AppConfig.Port), nil)
		}()
	})
}

func ProcessYoutubeDownloadForMessage(s *discordgo.Session, m *discordgo.MessageCreate, youtubeURL string, resolution string) {
	if strings.TrimSpace(resolution) == "" {
		resolution = "720p"
	}
	extension := "mp4"
	if resolution == "mp3" {
		extension = "mp3"
	}

	statusMsg, err := s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("▶️ `%s` **%s** 다운로드 요청 접수됨", youtubeURL, resolution))
	if err != nil {
		log.Printf("[yt-download-dm] ⚠️ 초기 메시지 전송 실패: %v", err)
		return
	}

	// 💡 3. mty(일반 메시지/DM) 환경에서도 동일하게 5개 제한 및 대기 처리 적용
	select {
	case downloadSemaphore <- struct{}{}:
		// 슬롯 여유 있음
	default:
		s.ChannelMessageEdit(m.ChannelID, statusMsg.ID, "⏳ **대기 중...**")
		downloadSemaphore <- struct{}{}
	}

	// 파이썬 다운로드 로직 진입 및 종료 시 채널 반환
	defer func() { <-downloadSemaphore }()

	firstFileName := fmt.Sprintf("ytdl_%d_%s_%s.%s", time.Now().Unix(), m.Author.ID, randomHex(2), extension)
	filePath := filepath.Join(config.AppConfig.TempDirectoryName, firstFileName)

	var pythonPath string
	if runtime.GOOS == "linux" {
		pythonPath = filepath.Join(".", ".venv", "bin", "python")
	} else {
		pythonPath = filepath.Join(".", ".venv", "Scripts", "python.exe")
	}
	cmd := exec.Command(pythonPath, "yt-downloader.py", youtubeURL, filePath, resolution, "--recode-video", "mp4")
	cmd.Env = append(os.Environ(), "PYTHONIOENCODING=utf-8", "PATH="+os.Getenv("PATH"))

	log.Printf("[yt-download-dm] %s 다운로드 시작 (기본파일명 후보: %s)", youtubeURL, firstFileName)

	var outBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &outBuf

	if err := cmd.Start(); err != nil {
		s.ChannelMessageEdit(m.ChannelID, statusMsg.ID, "❌ 오류 발생: 파이썬 프로세스 시작 실패")
		return
	}

	s.ChannelMessageEdit(m.ChannelID, statusMsg.ID, fmt.Sprintf("🔬 **`%s` 다운로드중...**", youtubeURL))

	waitErr := cmd.Wait()
	outputStr := outBuf.String()
	log.Printf("[Python DM Full Output]\n%s", outputStr)

	var meta VideoMeta
	var pythonError string
	var finalFileName string
	isPlaylist := false

	lines := strings.Split(outputStr, "\n")
	for _, line := range lines {
		text := strings.TrimSpace(line)
		if text == "" {
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
		if strings.HasPrefix(text, "SUCCESS:") {
			finalFileName = strings.TrimPrefix(text, "SUCCESS:")
		}
		if strings.HasPrefix(text, "METADATA:") {
			jsonData := strings.TrimPrefix(text, "METADATA:")
			_ = json.Unmarshal([]byte(jsonData), &meta)
		}
	}

	if isPlaylist || pythonError != "" || waitErr != nil || finalFileName == "" {
		var errMsg string
		if isPlaylist {
			errMsg = "▶️ 플레이리스트는 다운로드하지 못합니다. 개별 영상 주소를 입력해주세요."
		} else if pythonError != "" {
			errMsg = fmt.Sprintf("😭 원인: %s", pythonError)
		} else {
			errMsg = fmt.Sprintf("😭 다운로드 프로세스가 비정상적으로 종료되었거나 파일명을 반환받지 못했습니다.\n(WaitErr: %v)", waitErr)
		}

		errText := fmt.Sprintf("❌ **`%s` 다운로드 실패**\n%s", youtubeURL, errMsg)
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
		if err != nil {
			log.Printf("[yt-download-dm] ⚠️ 에러 메시지 및 재시도 버튼 전송 실패: %v", err)
		}
		return
	}

	realFilePath := filepath.Join(config.AppConfig.TempDirectoryName, finalFileName)
	expireDuration := time.Duration(config.AppConfig.ExpirySeconds) * time.Second
	expireTime := time.Now().Add(expireDuration)
	expireTimeStr := expireTime.Format("2006년 01월 02일 15:04")

	tracker := &FileTracker{
		filePath:   realFilePath,
		videoTitle: meta.Title,
		isExpired:  false,
		channelID:  m.ChannelID,
		resolution: resolution,
		expireTime: expireTimeStr,
		thumbnail:  meta.Thumbnail,
	}

	mapMutex.Lock()
	trackMap[finalFileName] = tracker
	mapMutex.Unlock()

	downloadURL := fmt.Sprintf("http://%s:%d/%s/%s", config.AppConfig.Domain, config.AppConfig.Port, config.AppConfig.TempDirectoryName, finalFileName)

	embed := &discordgo.MessageEmbed{
		Title:       "**☑️ 다운로드 준비 완료**",
		Description: fmt.Sprintf("**%s**\n\n[다운로드](%s)\n\n", meta.Title, downloadURL),
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
				Label:    "🗑️ 삭제",
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

	time.AfterFunc(expireDuration, func() {
		mapMutex.Lock()
		if tracker.isExpired {
			mapMutex.Unlock()
			return
		}
		tracker.isExpired = true
		delete(trackMap, finalFileName)
		mapMutex.Unlock()

		log.Printf("[yt-download-dm] %s 만료 타이머 작동", finalFileName)
		go func() {
			tracker.wg.Wait()
			// 이미 다운로드 완료로 인해 파일이 지워졌을 수 있으므로 확인 후 삭제
			if _, err := os.Stat(realFilePath); err == nil {
				if err := os.Remove(realFilePath); err != nil {
					log.Printf("[yt-download-dm] ⚠️ 파일 자동 파기 실패 (%s): %v", finalFileName, err)
				} else {
					log.Printf("[yt-download-dm] %s 파일 자동 파기 완료", finalFileName)
				}
			}

			expiredEmbed := &discordgo.MessageEmbed{
				Title:       "링크 만료됨",
				Description: fmt.Sprintf("~~%s~~\n\n다운로드 기간이 만료되었습니다.", meta.Title),
				Color:       0x404040,
			}

			// 💡 [교정 핵심] 일반 메시지 환경에서도 컴포넌트 배열을 비워 버튼을 강제 제거합니다.
			_, err = s.ChannelMessageEditComplex(&discordgo.MessageEdit{
				ID:         finalMsg.ID,
				Channel:    m.ChannelID,
				Embeds:     &[]*discordgo.MessageEmbed{expiredEmbed},
				Components: &[]discordgo.MessageComponent{}, // 💡 버튼 영구 삭제
			})
			if err != nil {
				log.Printf("[yt-download-dm] ⚠️ 만료 임베드 업데이트 실패: %v", err)
			}
		}()
	})
}

// 🧹 다운로드 실패 시 생성된 쓰레기/임시 파일들(.part, .webm, 코덱 분리 파일 등)을 삭제하는 함수
func cleanUpGarbageFiles(dirPath string, baseFileName string) {
	// 파일명에서 가장 뒤쪽 확장자를 제외한 순수 프리픽스 추출 (예: ytdl_1719144597_USERID_hex)
	ext := filepath.Ext(baseFileName)
	prefix := strings.TrimSuffix(baseFileName, ext)

	files, err := os.ReadDir(dirPath)
	if err != nil {
		log.Printf("[clean-up] ⚠️ 디렉토리 읽기 실패: %v", err)
		return
	}

	for _, file := range files {
		if file.IsDir() {
			continue
		}
		name := file.Name()

		// 해당 다운로드 세션 프리픽스(ytdl_시간_ID_해시)로 시작하는 파일들만 타겟팅
		if strings.HasPrefix(name, prefix) {
			// 💡 [핵심 교정 포인트]
			// 완전히 성공한 최종 결과물(.mp4 또는 .mp3) "그 자체"가 아니라면 전부 잔해로 간주하고 지웁니다.
			// 이 조건문은 .temp.mp4 나 .part.mp4 같은 중간 생성 파일들을 확실하게 걸러냅니다.
			if name != prefix+".mp4" && name != prefix+".mp3" {
				targetPath := filepath.Join(dirPath, name)
				if err := os.Remove(targetPath); err != nil {
					log.Printf("[clean-up] ⚠️ 잔해 파일 삭제 실패 (%s): %v", name, err)
				} else {
					log.Printf("[clean-up] 🧹 임시 잔해 파기 완료: %s", name)
				}
			}
		}
	}
}
