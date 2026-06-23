package messages

import (
	"ShimBot-D/utils"
	"fmt"
	"regexp"
	"strings"

	"github.com/bwmarrin/discordgo"
)

// MessageCreate 디스코드에 일반 메시지가 올라올 때마다 실행될 함수
func MessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {

	// 봇 자신이 보낸 메시지는 무시 (무한 루프 방지)
	if m.Author.ID == s.State.User.ID {
		return
	}

	// 앞뒤 불필요한 공백/줄바꿈 완벽 정제
	trimmedContent := strings.TrimSpace(m.Content)

	// 오직 DM 메시지일 때만 작동하는 영역
	if m.GuildID == "" {

		// 내부에 캡처 그룹 괄호()를 완전히 제거한 순수 패턴 문자열입니다.
		ytVideoPattern := `https?://(?:www\.)?(?:youtube\.com/(?:watch\?v=|embed/|shorts/)|youtu\.be/)[a-zA-Z0-9_-]{11}(?:[^\s<>"]*)`

		// 모든 종류의 유튜브 URL 패턴 (2번 케이스용)
		ytAnyPattern := `https?://(?:www\.)?(?:youtube\.com|youtu\.be)/[^\s<>"]+`

		// URL 덩어리 전체를 명확하게 첫 번째 ( ), 옵션을 두 번째 ( )로 분리했습니다.
		strictRegex := regexp.MustCompile(`^(` + ytVideoPattern + `)(?: +(1080|720|480|1080p|720p|480p|mp3))?$`)

		// [1번 케이스] 주소 단독이거나, 뒤에 화질/음원 옵션만 깔끔하게 붙어있는 경우
		if strictRegex.MatchString(trimmedContent) {
			matches := strictRegex.FindStringSubmatch(trimmedContent)

			videoURL := matches[1]
			option := matches[2]

			// 💡 1번 케이스에서도 주소 뒤에 &list= 가 붙어있다면 잘라내기
			if strings.Contains(videoURL, "watch?v=") && strings.Contains(videoURL, "&list=") {
				parts := strings.Split(videoURL, "&list=")
				videoURL = parts[0] // &list= 앞부분(단일 영상 주소)만 취함
			}

			// DM 채널 방 빌드 및 고정
			dmChannel, err := s.UserChannelCreate(m.Author.ID)
			if err != nil {
				return
			}
			m.ChannelID = dmChannel.ID

			switch option {
			case "":
				option = "720p"
			case "1080":
				option = "1080p"
			case "720":
				option = "720p"
			case "480":
				option = "480p"
			}

			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("개별 영상을 **%s**로 다운로드합니다...", option))

			// 백그라운드 다운로드 엔진 가동
			go utils.ProcessYoutubeDownloadForMessage(s, m, videoURL, option)
			return
		}

		// [2번 케이스] 평문 텍스트 내에 영상 주소나 플레이리스트 주소가 여러 개 섞여 있는 경우
		anyRegex := regexp.MustCompile(ytAnyPattern)
		allURLs := anyRegex.FindAllString(trimmedContent, -1)

		if len(allURLs) > 0 {
			// DM 채널 방 빌드
			dmChannel, err := s.UserChannelCreate(m.Author.ID)
			if err != nil {
				return
			}
			m.ChannelID = dmChannel.ID

			// 추출된 URL들을 순회하면서 &list= 매개변수를 완전히 제거합니다.
			var cleanedURLs []string
			for _, url := range allURLs {
				// 단, 재생목록 단독 주소(youtube.com/playlist?list=...)인 경우는 제외하고
				// watch?v= 가 포함된 혼합형 주소일 때만 &list= 뒷부분을 잘라냅니다.
				if strings.Contains(url, "watch?v=") && strings.Contains(url, "&list=") {
					parts := strings.Split(url, "&list=")
					url = parts[0] // 단일 영상 추출
				}
				cleanedURLs = append(cleanedURLs, url)
			}

			// 슬래시 명령어 인프라를 우회하기 위한 가짜(Dummy) 인터랙션 데이터 구성
			dummyInteraction := &discordgo.Interaction{
				ChannelID: dmChannel.ID,
				Token:     "msg_dm_" + m.ID,
			}

			s.ChannelMessageSend(m.ChannelID, "텍스트에서 유튜브 영상 링크를 추출합니다...")
			// 정제된 cleanedURLs 배열을 엔진에 전달합니다.
			go utils.ProcessMty(s, dummyInteraction, m.Author, cleanedURLs, 0, "dm", 0)

			return
		}

		return
	}

	// 2️⃣ 채널 메시지 분기 처리 (switch-case 및 소문자 정형화 처리)
	lowerContent := strings.ToLower(trimmedContent)

	switch lowerContent {
	case "안녕":
		s.ChannelMessageSend(m.ChannelID, "그래 잘 살고 있니?")
	case "你好":
		s.ChannelMessageSend(m.ChannelID, "你好！很高兴见到你。")
	case "hello":
		s.ChannelMessageSend(m.ChannelID, "Hello, how are you today?")
	case "hello, world":
		s.ChannelMessageSend(m.ChannelID, "참 아름다워라")
	}
}
