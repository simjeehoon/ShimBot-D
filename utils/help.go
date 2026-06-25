package utils

import (
	"strings"

	"github.com/bwmarrin/discordgo"
)

// 1. 명령어 정의 데이터
var HelpCommand = &discordgo.ApplicationCommand{
	Name:        "help",
	Description: "ShimBot-D의 사용법 및 명령어 도움말을 표시합니다.",
}

// 2. 명령어 실행 로직
func HandleHelp(s *discordgo.Session, i *discordgo.InteractionCreate) {
	var sb strings.Builder

	sb.WriteString("**ShimBot-D 는 무엇을 하나요?**\n")
	sb.WriteString("Developer Shim의 대리 로봇입니다.")
	sb.WriteString("어떤 콘텐츠등 **[공유] ➔ [디스코드] ➔ [ShimBot-D]**를 하면 데이터를 가공 및 처리합니다.\n\n")

	// 📌 기본 명령어 섹션
	sb.WriteString("⚙️ **명령어 안내(추가 예정**\n")
	sb.WriteString("> `/help` : 봇 사용법을 출력합니다.\n\n")
	sb.WriteString("> `/sdm` : 사용자와 dm을 시작합니다. 입력 후 안내에 따라 진행하시면 됩니다.\n\n")

	// 📌 DM 기반 핵심 동작 원리
	sb.WriteString("📥 **DM 처리**\n")

	sb.WriteString("1️⃣ **단일 유튜브 URL 전송 ➔ 즉시 다운로드**\n")
	sb.WriteString("• 유튜브 영상 주소를 DM으로 보내면 내부 가상환경(`yt-dlp`)이 분석 후 초고속 다운로드를 실행하여 파일 서버 링크를 제공합니다.\n")
	sb.WriteString("• **화질 커스텀 지정:** URL 바로 뒤에 한 칸 띄우고 원하는 화질을 함께 전송하면 옵션이 자동 적용됩니다.\n")
	sb.WriteString("  > *예시)* `https://youtu.be/... 1080p` (옵션 종류: `480p`, `720p`, `1080p`, `mp3`)\n\n")

	sb.WriteString("2️⃣ **유튜브 재생목록(Playlist) 또는 링크 믹스 장문 전송 ➔ 자동 추출 목록화**\n")
	sb.WriteString("• 플레이리스트 주소나 여러 유튜브 링크가 포함된 카톡 공지사항 등의 긴 평문을 DM으로 통째로 전송해 보세요.\n")
	sb.WriteString("• 봇이 텍스트 속에서 다른 잡다한 링크는 배제하고 **오직 유튜브 영상들만 정확하게 발라내어 리스트업**합니다.\n")
	sb.WriteString("• 가독성을 위해 10개 단위로 나누어진 페이지 UI가 제공되며, **인터랙션 선택 메뉴(Select Menu)**를 통해 원하는 영상들을 체크한 뒤 하단의 포맷 버튼(`480p/720p/1080p/MP3`)을 누르면 다운로드 대기열에 접수됩니다.\n\n")

	// 3. 인터랙션 응답 (Ephemeral 플래그로 개인 비서답게 본인에게만 표시)
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: sb.String(),
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
}
