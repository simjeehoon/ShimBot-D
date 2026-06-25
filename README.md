# 🤖 ShimBot-D (심봇드)

> Discord를 이용한 **개인 비서 봇**

## 🛠️ 기술 스택 (Tech Stack)

### **언어 및 라이브러리** 
1. Go (Golang 1.18+), `github.com/bwmarrin/discordgo`
2. Python, `yt-dlp`

### 사전 필수 환경
* `go`, `FFmpeg`, `Python`

## 🫡 `ShimBot-D`의 의의
- 개발자 Shim의 좌뇌 역할을 대리 수행합니다.
- 어떤 콘텐츠이든지 **공유 → 디스코드 → ShimBot-D**를 클릭하고 보내기를 누른다면, 필요한 작업을 알아서 진행합니다.
- 현재로서는 Youtube 개별 영상 링크를 받으면 Youtube를 다운로드해주며
- Youtube 플레이리스트 및 개별영상링크가 포함된 평문을 받으면 정확히 유튜브 영상만 고른 후 다운로드 메뉴가 나옵니다.
- 다운로드시 `480p`, `720p`, `1080p`, mp3의 옵션을 선택할 수 있습니다.

## 🧑‍💻 개발 진행 방향
- 모듈화가 간편하도록 utils/base.go에서 모든 핸들러를 등록하도록 설계했습니다.
- utils 내부에 기능을 추가합니다.

---

## ✨ 주요 기능 (Key Features)
* 명령어
  1. `/sdm`: dm을 시작합니다.
  2. `/help`: 도움말 페이지를 호출합니다.

* DM 옵션
  * **🔽 유튜브 다운**
    - `<유튜브 영상 URL>[ resolution]`을 봇에게 DM으로 보내보세요
    - `yt-dlp`로 영상을 서버에 다운로드하고 파일을 전송합니다.
  * **⏏️ 유튜브 목록 추출**
    - `<유튜브 or Playlist URL>`이 포함된 평문 텍스트를 DM으로 보내보세요.
    - **영상 다운로드를 위한 목록**을 생성해요.
      1. 개별영상은 그냥 담고,
      2. Playlist는 포함된 개별영상을 추출해서 담아요.
    - 목록에서 영상들을 선택하고, 원하는 화질의 영상 or mp3을 다운로드하세요.



## ⚙️ 설치 및 실행 방법 (Configuration)

Linux, Unix 등의 환경이라면?

`go build -o ShimBot-D`

Windows이라면?

`go build -o ShimBot-D.exe`

이후 서버 관리 모듈(example : `pm2`) 등을 이용하시거나 직접 ShimBot-D를 실행하면 됩니다.
* pm2를 이용한다면 `pm2 start ShimBot-D`(Windows라면 `ShimBot-D.exe`)

최초 실행 시 생성되는 `config.json` 파일에 서버 정보를 입력해주세요.

- `config.json` 에 입력해야 하는 주된 정보
  1. **도메인 (Domain)** 
      - 다운로드 완료 후 파일 링크 제공 등에 사용될 도메인 주소
  2. **포트 (Port)** 
      - 봇 웹서버 또는 파일 제공용 네트워크 포트
  3. 🔑**디스코드 봇 토큰 (Discord Bot Token)** 
      - 디스코드 개발자 포털에서 발급받은 봇 토큰
