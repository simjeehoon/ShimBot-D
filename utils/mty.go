package utils

import (
	"bytes"
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
	"unicode/utf8"

	"github.com/bwmarrin/discordgo"
)

type videoItem struct {
	Title string
	URL   string
	ID    string
}

var MtyCommand = &discordgo.ApplicationCommand{
	Name:        "mty",
	Description: "장문 글에서 개별 유튜브 URL들을 추출합니다. 10개씩 끊어서 출력합니다.",
	Options: []*discordgo.ApplicationCommandOption{
		{
			Type:        discordgo.ApplicationCommandOptionString,
			Name:        "content",
			Description: "유튜브 주소(또는 재생목록)가 포함된 카톡 or 긴 텍스트",
			Required:    true,
		},
		{
			Type:        discordgo.ApplicationCommandOptionString,
			Name:        "print_at",
			Description: "출력 위치를 지정합니다. (기본값: 나만보기)",
			Required:    false,
			Choices: []*discordgo.ApplicationCommandOptionChoice{
				{Name: "전체 공개", Value: "public"},
				{Name: "나만 보기", Value: "private"},
				{Name: "DM으로", Value: "dm"},
			},
		},
		{
			Type:        discordgo.ApplicationCommandOptionInteger,
			Name:        "count",
			Description: "출력할 URL의 최대 개수 (0 입력시 전부 출력, 기본값: 0)",
			Required:    false,
		},
	},
}

// 💡 데이터 경합 및 고루틴 안전성 확보를 위한 전역 RWMutex 추가
var (
	mtyGlobalStore = make(map[string][]videoItem)
	mtyMutex       sync.RWMutex
)

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

func HandleMty(s *discordgo.Session, i *discordgo.InteractionCreate) {
	options := i.ApplicationCommandData().Options
	optionMap := make(map[string]*discordgo.ApplicationCommandInteractionDataOption)
	for _, opt := range options {
		optionMap[opt.Name] = opt
	}

	content := optionMap["content"].StringValue()
	printAt := "private"
	count := 0

	if opt, exists := optionMap["print_at"]; exists {
		printAt = opt.StringValue()
	}

	if opt, exists := optionMap["count"]; exists {
		count = int(opt.IntValue())
		if count <= 0 {
			count = -1
		}
	}

	var responseFlags discordgo.MessageFlags
	if printAt == "private" {
		responseFlags = discordgo.MessageFlagsEphemeral
	}

	pattern := `https?://(?:www\.)?(?:youtube\.com|youtu\.be)/[^\s<>"]+`
	re := regexp.MustCompile(pattern)
	initialURLs := re.FindAllString(content, -1)

	if len(initialURLs) == 0 {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "❌ 입력하신 내용에서 Youtube URL을 찾지 못했습니다.",
				Flags:   responseFlags,
			},
		})
		return
	}

	if printAt == "dm" {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "📥 추출한 리스트를 DM(개인 메시지)으로 전송하고 있습니다...",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
	} else {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Flags: responseFlags,
			},
		})
	}

	userObj := i.User
	if i.Member != nil {
		userObj = i.Member.User
	}

	go ProcessMty(s, i.Interaction, userObj, initialURLs, count, printAt, responseFlags)
}

func ProcessMty(s *discordgo.Session, interaction *discordgo.Interaction, userObj *discordgo.User, urls []string, targetCount int, targetPrintAt string, flags discordgo.MessageFlags) {
	var finalVideos []videoItem

	for _, url := range urls {
		if strings.Contains(url, "youtube.com/playlist") || strings.Contains(url, "&list=") {
			playlistVideos, err := fetchPlaylistDetails(url)
			if err != nil {
				finalVideos = append(finalVideos, videoItem{Title: "⚠️ 재생목록 파싱 실패", URL: url, ID: ""})
			} else {
				finalVideos = append(finalVideos, playlistVideos...)
			}
		} else {
			title, id, err := fetchVideoTitleAndID(url)
			if err != nil {
				title = "⚠️ 제목 확인 불가"
			}
			if id == "" {
				id = fmt.Sprintf("fallback_%d", len(finalVideos))
			}
			finalVideos = append(finalVideos, videoItem{Title: title, URL: url, ID: id})
		}
	}

	if len(finalVideos) == 0 {
		errText := "❌ 유효한 유튜브 영상을 추출하지 못했습니다."
		if targetPrintAt != "dm" {
			s.InteractionResponseEdit(interaction, &discordgo.WebhookEdit{Content: &errText})
		}
		return
	}

	userID := userObj.ID

	// 💡 쓰기 락 획득
	mtyMutex.Lock()
	mtyGlobalStore[userID] = finalVideos
	mtyMutex.Unlock()

	targetChannelID := interaction.ChannelID
	if targetPrintAt == "dm" {
		dmChannel, err := s.UserChannelCreate(userObj.ID)
		if err != nil {
			log.Printf("DM 채널 생성 실패: %v", err)
			return
		}
		targetChannelID = dmChannel.ID
	}

	RenderMtyDisplay(s, interaction, userID, finalVideos, 0, []string{}, targetPrintAt, targetChannelID)
}

func RenderMtyDisplay(s *discordgo.Session, interaction *discordgo.Interaction, userID string, allVideos []videoItem, page int, customSelectedIDs []string, printAt string, channelID string) {
	pageSize := 10
	totalVideos := len(allVideos)
	totalPages := (totalVideos + pageSize - 1) / pageSize

	if page < 0 {
		page = 0
	}
	if page >= totalPages && totalPages > 0 {
		page = totalPages - 1
	}

	startIndex := page * pageSize
	endIndex := startIndex + pageSize
	if endIndex > totalVideos {
		endIndex = totalVideos
	}

	currentPageVideos := allVideos[startIndex:endIndex]

	// 💡 빈 슬라이스 수신 또는 nil 수신 시 기본 체크 처리 자동 생성
	if len(customSelectedIDs) == 0 {
		customSelectedIDs = []string{}
		for i, v := range currentPageVideos {
			if v.ID != "" {
				customSelectedIDs = append(customSelectedIDs, v.ID)
			} else {
				customSelectedIDs = append(customSelectedIDs, fmt.Sprintf("fallback_%d", startIndex+i))
			}
		}
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**Page (%d/%d)** 총 **%d**개 리스트 전개\n\n", page+1, totalPages, totalVideos))
	for idx, v := range currentPageVideos {
		sb.WriteString(formatVideoLine(startIndex+idx+1, v.Title, v.URL))
	}
	sb.WriteString(fmt.Sprintf("**Page (%d/%d) 항목을 고르고 버튼을 눌러주세요** \n\n", page+1, totalPages))
	contentText := sb.String()

	var selectOptions []discordgo.SelectMenuOption
	for i, v := range currentPageVideos {
		fixedID := v.ID
		if fixedID == "" {
			fixedID = fmt.Sprintf("fallback_%d", startIndex+i)
		}

		displayLabel := fmt.Sprintf("[%d] %s", startIndex+i+1, v.Title)
		if len(displayLabel) > 100 {
			displayLabel = displayLabel[:97] + "..."
		}

		isDefault := false
		for _, sid := range customSelectedIDs {
			if fixedID == sid {
				isDefault = true
				break
			}
		}

		selectOptions = append(selectOptions, discordgo.SelectMenuOption{
			Label:       displayLabel,
			Value:       fixedID,
			Description: v.URL,
			Default:     isDefault,
		})
	}

	var components []discordgo.MessageComponent

	if len(selectOptions) > 0 {
		components = append(components, discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.SelectMenu{
					CustomID:    fmt.Sprintf("mty_select_%s_%s", userID, printAt),
					Placeholder: "📥 다운로드할 영상들을 선택/해제 하세요",
					MinValues:   intPtr(1),
					MaxValues:   len(selectOptions),
					Options:     selectOptions,
				},
			},
		})
	}

	components = append(components, discordgo.ActionsRow{
		Components: []discordgo.MessageComponent{
			discordgo.Button{Label: "🎥 480p", Style: discordgo.PrimaryButton, CustomID: fmt.Sprintf("mty_btn_480p_%d_%s_%s", page, userID, printAt)},
			discordgo.Button{Label: "🎥 720p", Style: discordgo.PrimaryButton, CustomID: fmt.Sprintf("mty_btn_720p_%d_%s_%s", page, userID, printAt)},
			discordgo.Button{Label: "🎥 1080p", Style: discordgo.PrimaryButton, CustomID: fmt.Sprintf("mty_btn_1080p_%d_%s_%s", page, userID, printAt)},
			discordgo.Button{Label: "🎵 MP3", Style: discordgo.SuccessButton, CustomID: fmt.Sprintf("mty_btn_mp3_%d_%s_%s", page, userID, printAt)},
		},
	})

	prevPageLabel := "◀ 이전 (항목없음)"
	if page > 0 {
		prevPageLabel = fmt.Sprintf("◀ 이전 %d페이지", page)
	}

	nextPageLabel := "다음 (항목없음) ▶"
	if endIndex < totalVideos {
		nextPageLabel = fmt.Sprintf("다음 %d페이지 ▶", page+2)
	}

	components = append(components, discordgo.ActionsRow{
		Components: []discordgo.MessageComponent{
			discordgo.Button{
				Label:    prevPageLabel,
				Style:    discordgo.SecondaryButton,
				CustomID: fmt.Sprintf("mty_nav_prev_%d_%s_%s", page-1, userID, printAt),
				Disabled: page == 0,
			},
			discordgo.Button{
				Label:    nextPageLabel,
				Style:    discordgo.SecondaryButton,
				CustomID: fmt.Sprintf("mty_nav_next_%d_%s_%s", page+1, userID, printAt),
				Disabled: endIndex >= totalVideos,
			},
		},
	})

	if printAt == "dm" {
		if interaction.Message == nil {
			msg, err := s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
				Content:    contentText,
				Components: components,
			})
			if err == nil {
				interaction.Message = msg
			}
		} else {
			s.ChannelMessageEditComplex(&discordgo.MessageEdit{
				ID:         interaction.Message.ID,
				Channel:    channelID,
				Content:    &contentText,
				Components: &components,
			})
		}
	} else {
		s.InteractionResponseEdit(interaction, &discordgo.WebhookEdit{
			Content:    &contentText,
			Components: &components,
		})
	}
}

func HandleMtySelectMenu(s *discordgo.Session, i *discordgo.InteractionCreate, customID string) {
	// 💡 Deferred 처리로 변경하여 즉각 피드백 제공 및 먹통 방지
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredMessageUpdate,
	})

	raw := strings.TrimPrefix(customID, "mty_select_")
	parts := strings.Split(raw, "_")
	userID := parts[0]
	printAt := parts[1]

	selectedIDs := i.MessageComponentData().Values

	// 💡 읽기 락 획득
	mtyMutex.RLock()
	allVideos, exists := mtyGlobalStore[userID]
	mtyMutex.RUnlock()

	if !exists {
		return
	}

	var currentPage int
	if len(i.Message.Components) > 1 {
		if btnRow, ok := i.Message.Components[1].(*discordgo.ActionsRow); ok && len(btnRow.Components) > 0 {
			if btn, ok := btnRow.Components[0].(*discordgo.Button); ok {
				p := strings.Split(btn.CustomID, "_")
				if len(p) >= 4 {
					currentPage, _ = strconv.Atoi(p[3])
				}
			}
		}
	}

	RenderMtyDisplay(s, i.Interaction, userID, allVideos, currentPage, selectedIDs, printAt, i.ChannelID)
}

func HandleMtyNavigation(s *discordgo.Session, i *discordgo.InteractionCreate, customID string) {
	// 💡 Deferred 처리로 변경하여 즉각 피드백 제공 및 먹통 방지
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredMessageUpdate,
	})

	parts := strings.Split(customID, "_")
	if len(parts) < 6 {
		return
	}
	targetPage, _ := strconv.Atoi(parts[3])
	userID := parts[4]
	printAt := parts[5]

	// 💡 읽기 락 획득
	mtyMutex.RLock()
	allVideos, exists := mtyGlobalStore[userID]
	mtyMutex.RUnlock()

	if !exists {
		errText := "❌ 만료된 세션이거나 임시 데이터를 찾을 수 없습니다. 명령어를 다시 시도해주세요."
		s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content:    &errText,
			Components: &[]discordgo.MessageComponent{},
		})
		return
	}

	// 페이지를 바꿀 때는 선택 항목 상태 배열을 빈 리스트([]string{})로 넘겨 전체 선택을 재유도합니다.
	RenderMtyDisplay(s, i.Interaction, userID, allVideos, targetPage, []string{}, printAt, i.ChannelID)
}

func HandleMtyDownloadButton(s *discordgo.Session, i *discordgo.InteractionCreate, customID string) {
	dataStr := strings.TrimPrefix(customID, "mty_btn_")
	parts := strings.Split(dataStr, "_")

	quality := parts[0]
	printAt := parts[3]

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
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "❌ 선택된 영상이 없거나 양식을 읽지 못했습니다. 체크박스를 다시 확인해 주세요.",
			},
		})
		return
	}

	var responseFlags discordgo.MessageFlags
	if printAt != "dm" && printAt == "private" {
		responseFlags = discordgo.MessageFlagsEphemeral
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: fmt.Sprintf("⏬ 선택하신 총 **%d개**의 영상 다운로드를 대기열에 접수했습니다! (품질: `%s`)", len(videoIDs), quality),
			Flags:   responseFlags,
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
			fullURL = "https://www.youtube.com/watch?v=" + id
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
	venvDir := ".venv"
	var ytdlpPath string
	if runtime.GOOS == "windows" {
		ytdlpPath = filepath.Join(venvDir, "Scripts", "yt-dlp.exe")
	} else {
		ytdlpPath = filepath.Join(venvDir, "bin", "yt-dlp")
	}

	idPattern := regexp.MustCompile(`(?:v=|embed/|shorts/|youtu\.be/)([a-zA-Z0-9_-]{11})`)
	idMatch := idPattern.FindStringSubmatch(videoURL)
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

func fetchPlaylistDetails(playlistURL string) ([]videoItem, error) {
	venvDir := ".venv"
	var ytdlpPath string
	if runtime.GOOS == "windows" {
		ytdlpPath = filepath.Join(venvDir, "Scripts", "yt-dlp.exe")
	} else {
		ytdlpPath = filepath.Join(venvDir, "bin", "yt-dlp")
	}

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
	var results []videoItem

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
				URL:   fmt.Sprintf("https://www.youtube.com/watch?v=%s", id),
				ID:    id,
			})
		}
	}
	return results, nil
}

func intPtr(v int) *int { return &v }
