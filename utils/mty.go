package utils

import (
	"ShimBot-D/config"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/bwmarrin/discordgo"
)

type videoItem struct {
	Title string
	URL   string
	ID    string
}

// 💡 1. 상수를 활용하여 마법의 문자열(Magic String) 제거 및 기본 세팅 그룹화
const (
	pageSize    = 10
	fallbackID  = "fallback_%d"
	youtubeBase = "https://www.youtube.com/watch?v="
)

var (
	mtyGlobalStore = make(map[string][]videoItem)
	mtyMutex       sync.RWMutex
	ytIDPattern    = regexp.MustCompile(`(?:v=|embed/|shorts/|youtu\.be/)([a-zA-Z0-9_-]{11})`)
)

// getYtdlpPath는 현재 운영체제에 맞는 yt-dlp 실행 파일 경로를 반환합니다.
func getYtdlpPath() string {
	venvDir := ".venv"
	if runtime.GOOS == "windows" {
		return filepath.Join(venvDir, "Scripts", "yt-dlp.exe")
	}
	return filepath.Join(venvDir, "bin", "yt-dlp")
}

// generateSessionID는 16글자 Hex 문자열을 생성하여 세션 식별자로 사용합니다.
func generateSessionID() string {
	b := make([]byte, 8) // 16글자 Hex 문자열 획득
	_, err := rand.Read(b)
	if err != nil {
		// 예외 상황 시 대체 수단으로 타임스탬프 기반 생성
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// formatVideoLine는 제한된 길이 내에서 영상 제목과 URL을 포맷팅하여 반환합니다.
func formatVideoLine(idx int, title string, url string) string {
	maxTitleBytes := 190 - len(url)
	if maxTitleBytes < 30 {
		maxTitleBytes = 30
	}
	if len(title) > maxTitleBytes {
		title = title[:maxTitleBytes]
		for !utf8.ValidString(title) && len(title) > 0 {
			title = title[:len(title)-1]
		}
		title += "..."
	}
	return fmt.Sprintf("[%d] **%s**\n`%s`\n", idx, title, url)
}

// 외부에서 호출하는 함수, 유튜브 URL 리스트를 받아서 각 URL을 처리하고, select menu와 버튼을 포함한 DM 메시지를 유저에게 전송합니다.
func FetchVideos(s *discordgo.Session, userID string, urls []string) {
	var videoItems []videoItem

	for _, url := range urls {
		if strings.Contains(url, "youtube.com/playlist") || strings.Contains(url, "&list=") {
			playlistVideos, err := listToVideoItems(url)
			if err != nil {
				videoItems = append(videoItems, videoItem{Title: "⚠️ 재생목록 파싱 실패", URL: url, ID: ""})
			} else {
				videoItems = append(videoItems, playlistVideos...)
			}
		} else {
			title, id, err := fetchVideoTitleAndID(url)
			if err != nil {
				title = "⚠️ 제목 확인 불가"
			}
			if id == "" {
				id = fmt.Sprintf(fallbackID, len(videoItems))
			}
			videoItems = append(videoItems, videoItem{Title: title, URL: url, ID: id})
		}
	}

	dmChannel, err := s.UserChannelCreate(userID)
	if err != nil {
		log.Printf("DM 채널 생성 실패 (User: %s): %v", userID, err)
		return
	}

	if len(videoItems) == 0 {
		errText := "❌ 유효한 유튜브 영상을 추출하지 못했습니다."
		_, _ = s.ChannelMessageSend(dmChannel.ID, errText)
		return
	}

	// 💡 조합키 정의: 명확한 가독성을 제공하는 접두사 조합 (쉼표 구분 방식을 채택하여 _ 분리 영향 방지)
	sessionID := generateSessionID()
	storeKey := fmt.Sprintf("SID:%s,UID:%s", sessionID, userID)

	mtyMutex.Lock()
	mtyGlobalStore[storeKey] = videoItems
	mtyMutex.Unlock()

	// 만료시간 이후 세션 데이터를 제거하는 타이머 설정 (config.json에서 ExpirySeconds 값 활용)
	time.AfterFunc(time.Duration(config.AppConfig.ExpirySeconds)*time.Second, func() {
		mtyMutex.Lock()
		delete(mtyGlobalStore, storeKey)
		mtyMutex.Unlock()
		log.Printf("[mty] %d초가 경과하여 세션 데이터 (%s)를 제거했습니다.", config.AppConfig.ExpirySeconds, storeKey)
	})

	// CustomID 연계를 위해 storeKey(조합키) 전체를 전달합니다.
	RenderMtySelectMenu(s, nil, dmChannel.ID, storeKey, videoItems, 0, []string{}, "")
}

// RenderMtySelectMenu 로 컬 메시지 또는 Interaction 응답을 통해 선택 메뉴와 버튼을 포함한 유튜브 영상 리스트를 DM으로 전송합니다.
func RenderMtySelectMenu(s *discordgo.Session, interaction *discordgo.Interaction, targetChannelID string, storeKey string, allVideos []videoItem, page int, customSelectedIDs []string, messageID string) {
	totalVideosCnt := len(allVideos)
	totalPages := (totalVideosCnt + pageSize - 1) / pageSize

	if page < 0 {
		page = 0
	}
	if page >= totalPages && totalPages > 0 {
		page = totalPages - 1
	}

	startIndex := page * pageSize
	endIndex := startIndex + pageSize
	if endIndex > totalVideosCnt {
		endIndex = totalVideosCnt
	}

	currentPageVideos := allVideos[startIndex:endIndex]

	if len(customSelectedIDs) == 0 {
		customSelectedIDs = make([]string, 0, len(currentPageVideos))
		for i, v := range currentPageVideos {
			if v.ID != "" {
				customSelectedIDs = append(customSelectedIDs, v.ID)
			} else {
				customSelectedIDs = append(customSelectedIDs, fmt.Sprintf(fallbackID, startIndex+i))
			}
		}
	}

	selectedMap := make(map[string]struct{}, len(customSelectedIDs))
	for _, sid := range customSelectedIDs {
		selectedMap[sid] = struct{}{}
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("🎥 **Page (%d/%d)** (총 **%d**개)\n\n", page+1, totalPages, totalVideosCnt))
	for idx, v := range currentPageVideos {
		sb.WriteString(formatVideoLine(startIndex+idx+1, v.Title, v.URL))
	}
	sb.WriteString(fmt.Sprintf("🎥 **Page (%d/%d)** (총 **%d**개)\n\n", page+1, totalPages, totalVideosCnt))
	contentText := sb.String()

	var selectOptions []discordgo.SelectMenuOption
	for i, v := range currentPageVideos {
		fixedID := v.ID
		if fixedID == "" {
			fixedID = fmt.Sprintf(fallbackID, startIndex+i)
		}

		displayLabel := fmt.Sprintf("[%d] %s", startIndex+i+1, v.Title)
		if len(displayLabel) > 100 {
			displayLabel = displayLabel[:97] + "..."
		}

		_, isDefault := selectedMap[fixedID]

		selectOptions = append(selectOptions, discordgo.SelectMenuOption{
			Label:       displayLabel,
			Value:       fixedID,
			Description: v.URL,
			Default:     isDefault,
		})
	}

	var components []discordgo.MessageComponent

	// CustomID 규칙 정립: mty_select_{storeKey}_dm
	if len(selectOptions) > 0 {
		components = append(components, discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.SelectMenu{
					CustomID:    fmt.Sprintf("mty_select_%s_dm", storeKey),
					Placeholder: "📥 다운로드할 영상들을 선택/해제 하세요",
					MinValues:   intPtr(1),
					MaxValues:   len(selectOptions),
					Options:     selectOptions,
				},
			},
		})
	}

	// 다운로드 버튼 포맷 규격화: mty_btn_{quality}_{page}_{storeKey}_dm
	components = append(components, discordgo.ActionsRow{
		Components: []discordgo.MessageComponent{
			discordgo.Button{Label: "🎥 480p", Style: discordgo.PrimaryButton, CustomID: fmt.Sprintf("mty_btn_480p_%d_%s_dm", page, storeKey)},
			discordgo.Button{Label: "🎥 720p", Style: discordgo.PrimaryButton, CustomID: fmt.Sprintf("mty_btn_720p_%d_%s_dm", page, storeKey)},
			discordgo.Button{Label: "🎥 1080p", Style: discordgo.PrimaryButton, CustomID: fmt.Sprintf("mty_btn_1080p_%d_%s_dm", page, storeKey)},
			discordgo.Button{Label: "🎵 MP3", Style: discordgo.SuccessButton, CustomID: fmt.Sprintf("mty_btn_mp3_%d_%s_dm", page, storeKey)},
		},
	})

	prevPageLabel := "◀ 이전 (항목없음)"
	if page > 0 {
		prevPageLabel = fmt.Sprintf("◀ 이전 %d페이지", page)
	}

	nextPageLabel := "다음 (항목없음) ▶"
	if endIndex < totalVideosCnt {
		nextPageLabel = fmt.Sprintf("다음 %d페이지 ▶", page+2)
	}

	// 내비게이션 버튼 포맷 규격화: mty_nav_{direction}_{targetPage}_{storeKey}_dm
	components = append(components, discordgo.ActionsRow{
		Components: []discordgo.MessageComponent{
			discordgo.Button{
				Label:    prevPageLabel,
				Style:    discordgo.SecondaryButton,
				CustomID: fmt.Sprintf("mty_nav_prev_%d_%s_dm", page-1, storeKey),
				Disabled: page == 0,
			},
			discordgo.Button{
				Label:    nextPageLabel,
				Style:    discordgo.SecondaryButton,
				CustomID: fmt.Sprintf("mty_nav_next_%d_%s_dm", page+1, storeKey),
				Disabled: endIndex >= totalVideosCnt,
			},
		},
	})

	if interaction == nil {
		if messageID == "" { // 새 메시지 전송
			_, _ = s.ChannelMessageSendComplex(targetChannelID, &discordgo.MessageSend{
				Content:    contentText,
				Components: components,
			})
		} else { // 기존 메시지 수정
			_, _ = s.ChannelMessageEditComplex(&discordgo.MessageEdit{
				ID:         messageID,
				Channel:    targetChannelID,
				Content:    &contentText,
				Components: &components,
			})
		}
	} else { // Interaction 응답 수정
		_, _ = s.InteractionResponseEdit(interaction, &discordgo.WebhookEdit{
			Content:    &contentText,
			Components: &components,
		})
	}
}

func HandleMtySelectMenu(s *discordgo.Session, i *discordgo.InteractionCreate, customID string) {
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredMessageUpdate,
	})

	// 💡 "mty_select_" 접두사와 뒤의 "_dm" 접미사를 걷어내면 순수한 storeKey가 원형 복원됩니다.
	raw := strings.TrimPrefix(customID, "mty_select_")
	storeKey := strings.TrimSuffix(raw, "_dm")

	selectedIDs := i.MessageComponentData().Values

	mtyMutex.RLock()
	allVideos, exists := mtyGlobalStore[storeKey]
	mtyMutex.RUnlock()

	if !exists {
		return
	}

	var currentPage int
	if len(i.Message.Components) > 1 {
		if btnRow, ok := i.Message.Components[1].(*discordgo.ActionsRow); ok && len(btnRow.Components) > 0 {
			if btn, ok := btnRow.Components[0].(*discordgo.Button); ok {
				p := strings.Split(btn.CustomID, "_")
				// 버튼 포맷: mty_btn_480p_{page}_{storeKey}_dm
				// _로 스플릿 시 앞쪽 고정 인덱스 3번이 무조건 page 번호가 됩니다.
				if len(p) >= 4 {
					currentPage, _ = strconv.Atoi(p[3])
				}
			}
		}
	}

	RenderMtySelectMenu(s, i.Interaction, i.ChannelID, storeKey, allVideos, currentPage, selectedIDs, "")
}

func HandleMtyNavigation(s *discordgo.Session, i *discordgo.InteractionCreate, customID string) {
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredMessageUpdate,
	})

	// 포맷: mty_nav_{prev/next}_{targetPage}_{storeKey}_dm
	// 정규 필터를 거치기 위해 접두사와 접미사를 먼저 정제합니다.
	raw := strings.TrimPrefix(customID, "mty_nav_")
	raw = strings.TrimSuffix(raw, "_dm")

	// 남은 덩어리: {prev/next}_{targetPage}_{storeKey}
	// storeKey 내부에는 쉼표(,)가 있으므로 언더바(_) 분리가 유효합니다.
	parts := strings.SplitN(raw, "_", 3)
	if len(parts) < 3 {
		return
	}

	targetPage, _ := strconv.Atoi(parts[1])
	storeKey := parts[2] // 복원된 "SID:...,UID:..."

	mtyMutex.RLock()
	allVideos, exists := mtyGlobalStore[storeKey]
	mtyMutex.RUnlock()

	if !exists {
		errText := "❌ 만료된 세션이거나 데이터를 찾을 수 없습니다."
		_, _ = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content:    &errText,
			Components: &[]discordgo.MessageComponent{},
		})
		return
	}

	RenderMtySelectMenu(s, i.Interaction, i.ChannelID, storeKey, allVideos, targetPage, []string{}, "")
}

func HandleMtyDownloadButton(s *discordgo.Session, i *discordgo.InteractionCreate, customID string) {
	// 포맷: mty_btn_{quality}_{page}_{storeKey}_dm
	raw := strings.TrimPrefix(customID, "mty_btn_")
	raw = strings.TrimSuffix(raw, "_dm")

	// 남은 덩어리: {quality}_{page}_{storeKey}
	parts := strings.SplitN(raw, "_", 3)
	if len(parts) < 3 {
		return
	}
	quality := parts[0]

	var videoIDs []string
	if len(i.Message.Components) > 0 {
		if selectRow, ok := i.Message.Components[0].(*discordgo.ActionsRow); ok && len(selectRow.Components) > 0 {
			if selMenu, ok := selectRow.Components[0].(*discordgo.SelectMenu); ok {
				for _, opt := range selMenu.Options {
					if opt.Default {
						videoIDs = append(videoIDs, opt.Value)
					}
				}
			}
		}
	}

	if len(videoIDs) == 0 {
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "❌ 선택된 영상이 없거나 양식을 읽지 못했습니다. 체크박스를 다시 확인해 주세요.",
			},
		})
		return
	}

	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: fmt.Sprintf("⏬ 총 **%d개**의 영상을 다운로드하겠습니다. (품질: `%s`)", len(videoIDs), quality),
		},
	})

	var targetUser *discordgo.User
	if i.User != nil {
		targetUser = i.User
	} else if i.Member != nil {
		targetUser = i.Member.User
	}

	for _, id := range videoIDs {
		if strings.TrimSpace(id) == "" {
			continue
		}

		var fullURL string
		if strings.HasPrefix(id, "fallback_") || strings.HasPrefix(id, "unknown_") {
			fullURL = id
		} else {
			fullURL = youtubeBase + id
		}

		mockMsg := &discordgo.MessageCreate{
			Message: &discordgo.Message{
				ChannelID: i.ChannelID,
				Author:    targetUser,
			},
		}

		go ProcessYoutubeDownloadForMessage(s, mockMsg, fullURL, quality)
	}
}

func fetchVideoTitleAndID(videoURL string) (string, string, error) {
	ytdlpPath := getYtdlpPath()

	idMatch := ytIDPattern.FindStringSubmatch(videoURL)
	extractedID := ""
	if len(idMatch) > 1 {
		extractedID = idMatch[1]
	}

	cmd := exec.Command(ytdlpPath, "--get-title", videoURL)
	cmd.Env = append(os.Environ(), "PYTHONIOENCODING=utf-8")

	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return "유튜브 단일 영상 (" + extractedID + ")", extractedID, nil
	}
	return strings.TrimSpace(stdout.String()), extractedID, nil
}

func listToVideoItems(playlistURL string) ([]videoItem, error) {
	ytdlpPath := getYtdlpPath()

	args := []string{"--flat-playlist", "--print", "%(title)s\t%(id)s", playlistURL}
	cmd := exec.Command(ytdlpPath, args...)
	cmd.Env = append(os.Environ(), "PYTHONIOENCODING=utf-8")

	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return nil, err
	}

	output := strings.TrimSpace(stdout.String())
	if output == "" {
		return []videoItem{}, nil
	}

	lines := strings.Split(output, "\n")
	results := make([]videoItem, 0, len(lines))

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.Split(line, "\t")
		if len(parts) >= 2 {
			title := strings.Join(parts[:len(parts)-1], "\t")
			id := parts[len(parts)-1]

			results = append(results, videoItem{
				Title: title,
				URL:   youtubeBase + id,
				ID:    id,
			})
		}
	}
	return results, nil
}

func intPtr(v int) *int { return &v }
