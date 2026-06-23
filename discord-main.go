package main

import (
	"fmt"
	"log"

	"os"
	"os/signal"
	"syscall"

	"ShimBot-D/config"
	"ShimBot-D/messages"
	"ShimBot-D/utils"

	"github.com/bwmarrin/discordgo"
)

// 하위 폴더 패키지들을 임포트합니다.

func main() {
	fmt.Println("[ShimBot-D] 서버를 시작합니다.")

	// 봇 시작 전 환경 검사 및 가상환경 생성 자동화
	if err := utils.CheckAndSetupEnvironment(); err != nil {
		log.Fatalf("초기 환경 세팅 중 오류 발생: %v", err)
	}

	// 1. 설정 파일 로드
	if err := config.LoadConfig(); err != nil {
		log.Fatal("config.json 에러: ", err)
	}

	// 2. 디스코드 세션 생성
	dg, err := discordgo.New("Bot " + config.AppConfig.Token)
	if err != nil {
		log.Fatal("Failed to create Discord session: ", err)
	}

	// 3. 이벤트 핸들러 등록
	dg.AddHandler(utils.InteractionCreate)
	dg.AddHandler(messages.MessageCreate)

	// 4. 게이트웨이 인텐트 설정
	dg.Identify.Intents = discordgo.IntentsGuildMessages |
		discordgo.IntentsDirectMessages |
		discordgo.IntentsMessageContent

	// 🚀 [수정] 디스코드가 열리기 전에 파일 서버를 먼저 완벽하게 가동합니다.
	utils.StartFileServer(dg)
	fmt.Println("[HTTP 서버] 로컬 파일 서버가 가동되었습니다.")

	// 5. 디스코드 웹소켓 연결 (로그인)
	err = dg.Open()
	if err != nil {
		fmt.Println("[디스코드] 연결 오류: ", err)
		return
	}

	// 연결 성공 직후 defer를 등록하면, 프로그램이 어떻게 끝나든 안전하게 Close가 보장됩니다.
	defer func() {
		fmt.Println("\n[디스코드] 봇을 안전하게 종료합니다.")
		dg.Close()
	}()

	// 💡 6. 파일 서버 시작 (디스코드 연결 성공 후 dg 세션을 넘겨서 실행)
	utils.StartFileServer(dg)

	// 7. 디스코드 서버에 슬래시 명령어 등록 (commands/base.go에 정의된 리스트 활용)
	fmt.Println("[빗금 명령어] 일괄 동기화 중...")
	cmds, err := dg.ApplicationCommandBulkOverwrite(dg.State.User.ID, "", utils.AllCommands)
	if err != nil {
		fmt.Printf("[빗금 명령어] 일괄 등록 실패: %v\n", err)
	} else {
		fmt.Printf("[빗금 명령어] 총 %d개의 명령어 등록 및 동기화 완료\n", len(cmds))
	}

	// 8. 프로그램이 바로 종료되지 않도록 시그널 대기
	fmt.Println("[ShimBot-D] 성공적으로 실행되었습니다.")

	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-sc
}
