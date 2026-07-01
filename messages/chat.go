package messages

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/bwmarrin/discordgo"
)

var (
	strictRegex = regexp.MustCompile(`^(https?://(?:www\.)?(?:youtube\.com/(?:watch\?v=|embed/|shorts/)|youtu\.be/)[a-zA-Z0-9_-]{11}(?:[^\s<>"]*))(?: +(1080|720|480|1080p|720p|480p|mp3))?$`)
	anyRegex    = regexp.MustCompile(`https?://(?:www\.)?(?:youtube\.com|youtu\.be)/[^\s<>"]+`)
	IsDebugMode = true
)

// 디버그 함수
func DebugSend(s *discordgo.Session, m *discordgo.MessageCreate, channelID, message string) {
	if IsDebugMode {
		s.ChannelMessageSend(channelID, message)
	}
}

// DM 채널 생성 및 가져오기
func getOrCreateDMChannel(s *discordgo.Session, userID string) (string, error) {
	dm, err := s.UserChannelCreate(userID)
	if err != nil {
		return "", err
	}
	return dm.ID, nil
}

// 유튜브 영상 URL에서 플레이리스트 파라미터 제거
func stripPlaylistParam(url string) string {
	if strings.Contains(url, "watch?v=") && strings.Contains(url, "&list=") {
		return strings.Split(url, "&list=")[0]
	}
	return url
}

// 단일 영상 처리
func handleSingleVideo(s *discordgo.Session, m *discordgo.MessageCreate, url, option string) {
	videoURL := stripPlaylistParam(url)

	normalizeOption := func(opt string) string {
		switch opt {
		case "1080", "1080p":
			return "1080p"
		case "480", "480p":
			return "480p"
		case "mp3":
			return "mp3"
		default:
			return "720p" // 기본값 720p
		}
	}

	option = normalizeOption(option)

	channelID, err := getOrCreateDMChannel(s, m.Author.ID)
	if err != nil {
		return
	}

	DebugSend(s, m, channelID, fmt.Sprintf("개별영상, URL: %s, 옵션: %s", videoURL, option))
	//go utils.ProcessYoutubeDownloadForMessage(s, m, videoURL, option)
}

// 다중 영상 처리
func handleMultiVideo(s *discordgo.Session, m *discordgo.MessageCreate, urls []string) {
	channelID, err := getOrCreateDMChannel(s, m.Author.ID)
	if err != nil {
		return
	}

	var cleanedURLs []string
	for _, url := range urls {
		cleanedURLs = append(cleanedURLs, stripPlaylistParam(url))
	}

	joinedURLs := strings.Join(cleanedURLs, "\n")
	DebugSend(s, m, channelID, fmt.Sprintf("다중영상, URL: %s", joinedURLs))
	//dummyInteraction := &discordgo.Interaction{ChannelID: channelID, Token: "msg_dm_" + m.ID}
	//go utils.ProcessMty(s, dummyInteraction, m.Author, cleanedURLs, 0, "dm", 0)
}

// 메인 플로우
func MessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.GuildID != "" {
		return // DM이 아니면 종료
	}

	if m.Author.ID == s.State.User.ID {
		return // 자기 자신의 메시지는 무시
	}

	trimmedContent := strings.TrimSpace(m.Content)

	// 1. 단일 영상 처리
	if matches := strictRegex.FindStringSubmatch(trimmedContent); matches != nil {
		handleSingleVideo(s, m, matches[1], matches[2])
		return
	}

	// 2. 다중 URL 추출 처리
	if allURLs := anyRegex.FindAllString(trimmedContent, -1); len(allURLs) > 0 {
		handleMultiVideo(s, m, allURLs)
		return
	}
}
