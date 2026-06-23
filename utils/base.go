package utils

import (
	"log"
	"strings"

	"github.com/bwmarrin/discordgo"
)

var AllCommands = []*discordgo.ApplicationCommand{
	//YtCommand,
	//MtyCommand,
	HelpCommand,
}

var CommandHandlers = map[string]func(s *discordgo.Session, i *discordgo.InteractionCreate){
	//"yt":   HandleYt,
	//"mty":  HandleMty,
	"help": HandleHelp,
}

var ButtonHandlers = map[string]func(s *discordgo.Session, i *discordgo.InteractionCreate, customID string){
	"delete_file_": HandleYtDeleteButton,
	"yt_retry_":    HandleYtRetryButton, // 👈 여기에 재시도 버튼 핸들러를 등록합니다.
	"mty_btn_":     HandleMtyDownloadButton,
	"mty_nav_":     HandleMtyNavigation,
}

var SelectHandlers = map[string]func(s *discordgo.Session, i *discordgo.InteractionCreate, customID string){
	"mty_select_": HandleMtySelectMenu,
}

func InteractionCreate(s *discordgo.Session, i *discordgo.InteractionCreate) {
	// 예기치 못한 구버전 상수 불일치 패닉(Panic)이 터져 봇 전체가 멈추는 것을 철저히 방어
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[Panic Recovered] 상호작용 라우터 이벤트 루프 실시간 보호됨: %v", r)
		}
	}()

	switch i.Type {
	case discordgo.InteractionApplicationCommand:
		commandData := i.ApplicationCommandData()
		if handler, exists := CommandHandlers[commandData.Name]; exists {
			handler(s, i)
		}

	case discordgo.InteractionMessageComponent:
		customID := i.MessageComponentData().CustomID

		// 1. 셀렉트 박스(체크박스) 이벤트 우선 파싱 라우팅
		for prefix, handler := range SelectHandlers {
			if strings.HasPrefix(customID, prefix) {
				handler(s, i, customID)
				return
			}
		}

		// 2. 일반 버튼 및 네비게이션용 버튼 이벤트 라우팅
		for prefix, handler := range ButtonHandlers {
			if strings.HasPrefix(customID, prefix) {
				handler(s, i, customID)
				return
			}
		}
	}
}
