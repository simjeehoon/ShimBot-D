package utils

import (
	"log"
	"strings"

	"github.com/bwmarrin/discordgo"
)

var AllCommands = []*discordgo.ApplicationCommand{
	HelpCommand,
	SdmCommand,
}

var CommandHandlers = map[string]func(s *discordgo.Session, i *discordgo.InteractionCreate){
	"help": HandleHelp,
	"sdm":  HandleSdm,
}

var ButtonHandlers = map[string]func(s *discordgo.Session, i *discordgo.InteractionCreate, customID string){
	"delete_file_": HandleYtDeleteButton,
	"yt_retry_":    HandleYtRetryButton,
	"mty_btn_":     HandleMtyDownloadButton,
	"mty_nav_":     HandleMtyNavigation,
}

var SelectHandlers = map[string]func(s *discordgo.Session, i *discordgo.InteractionCreate, customID string){
	"mty_select_": HandleMtySelectMenu,
}

func InteractionCreate(s *discordgo.Session, i *discordgo.InteractionCreate) {
	// 예기치 못한 패닉(Panic) 방어
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

		// 라우팅 헬퍼 함수를 통해 Select -> Button 순으로 처리
		if handleComponentEvent(s, i, customID, SelectHandlers) {
			return
		}
		if handleComponentEvent(s, i, customID, ButtonHandlers) {
			return
		}
	}
}

// handleComponentEvent는 접두사(Prefix) 기반으로 핸들러를 찾아 실행하는 공통 헬퍼 함수입니다.
func handleComponentEvent(s *discordgo.Session, i *discordgo.InteractionCreate, customID string, handlers map[string]func(*discordgo.Session, *discordgo.InteractionCreate, string)) bool {
	for prefix, handler := range handlers {
		if strings.HasPrefix(customID, prefix) {
			handler(s, i, customID)
			return true // 처리 완료
		}
	}
	return false // 일치하는 핸들러 없음
}
