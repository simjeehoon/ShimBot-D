package utils

import (
	"log"

	"github.com/bwmarrin/discordgo"
)

// 1. 명령어 정의 데이터 (모든 공간에서 실행 가능하도록 열어둠)
var SdmCommand = &discordgo.ApplicationCommand{
	Name:        "sdm",
	Description: "ShimBot-D과 DM을 시작합니다.",

	// 사용자 설치(User Install) 지원 설정
	IntegrationTypes: &[]discordgo.ApplicationIntegrationType{
		discordgo.ApplicationIntegrationUserInstall,
	},

	// 어디서든 이 명령어를 칠 수 있도록 허용 (서버, 봇 DM, 일반 DM 방 모두 포함)
	Contexts: &[]discordgo.InteractionContextType{
		discordgo.InteractionContextGuild,
		discordgo.InteractionContextBotDM,
		discordgo.InteractionContextPrivateChannel,
	},
}

// 2. 명령어 실행 로직
func HandleSdm(s *discordgo.Session, i *discordgo.InteractionCreate) {
	// 안전하게 봇의 Client ID(애플리케이션 ID) 가져오기
	botID := s.State.User.ID

	// 디스코드에서 가장 에러 없이 작동하는 '봇 프로필 팝업'용 공식 딥링크
	profileURL := "https://discord.com/users/" + botID

	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: "**[ShimBot-D 프로필 열기]** 버튼을 누른 뒤, \n팝업 창에서 \n[💬] 혹은 [💬메시지] 버튼을 누르면\n DM을 시작하실 수 있어요!",

			// 에러 방지를 위해 명령어를 입력한 본인에게만 메시지가 보이도록 처리 (Ephemeral)
			Flags: discordgo.MessageFlagsEphemeral,

			// 버튼 컴포넌트 추가
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.Button{
							Label: "ShimBot-D 프로필 열기",
							Style: discordgo.LinkButton, // 외부 링크 형태로 작동하는 버튼
							URL:   profileURL,           // 프로필 오픈 딥링크
						},
					},
				},
			},
		},
	})

	if err != nil {
		log.Printf("[Error] DM 명령어 응답 중 실패: %v", err)
	}
}
