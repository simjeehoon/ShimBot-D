package messages

import (
	"ShimBot-D/utils"
	"regexp"
	"strings"

	"github.com/bwmarrin/discordgo"
)

var (
	strictRegex = regexp.MustCompile(`^(https?://(?:www\.)?(?:youtube\.com/(?:watch\?v=|embed/|shorts/)|youtu\.be/)[a-zA-Z0-9_-]{11}(?:[^\s<>"]*))(?: +(1080|720|480|1080p|720p|480p|mp3))?$`)
	anyRegex    = regexp.MustCompile(`https?://(?:www\.)?(?:youtube\.com|youtu\.be)/[^\s<>"]+`)
	IsDebugMode = true
)

func send(s *discordgo.Session, m *discordgo.MessageCreate, message string) error {
	dm, err := s.UserChannelCreate(m.Author.ID)
	if err != nil {
		return err
	}

	if IsDebugMode {
		_, err = s.ChannelMessageSend(dm.ID, message)
		if err != nil {
			return err
		}
	}
	return nil
}

// 메인 플로우
func MessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.GuildID != "" {
		return // DM이 아니면 종료
	}

	if m.Author.ID == s.State.User.ID {
		return // 자기 자신의 메시지는 무시합니다
	}

	trimmedContent := strings.TrimSpace(m.Content)

	// 1. 단일 영상 처리
	if matches := strictRegex.FindStringSubmatch(trimmedContent); matches != nil {
		url := matches[1]

		if strings.Contains(url, "watch?v=") && strings.Contains(url, "&list=") {
			url = strings.Split(url, "&list=")[0]
		}

		option := matches[2]
		// 옵션 처리
		switch option {
		case "1080", "1080p":
			option = "1080p"
		case "480", "480p":
			option = "480p"
		case "mp3":
			option = "mp3"
		default:
			option = "720p" // 기본값 720p
		}

		go utils.ProcessYoutubeDownloadForMessage(s, m, url, option)
		return
	}

	// 2. 다중 URL 추출 처리
	if allURLs := anyRegex.FindAllString(trimmedContent, -1); len(allURLs) > 0 {
		send(s, m, "👨‍💻 텍스트에서 개별 유튜브 영상을 분석할게요...")
		go utils.FetchVideos(s, m.Author.ID, allURLs)
		return
	}
}
