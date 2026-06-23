import sys
import json
import yt_dlp
import threading
import os
import sys

# 인자 개수가 늘어나도 에러 안 나게 안전하게 슬라이싱으로 처리
url = sys.argv[1]
out_path = sys.argv[2]
resolution = sys.argv[3]

# 타임아웃 발생 시 호출될 함수
def timeout_handler():
    print("ERROR:응답이 없어 다운로드가 취소되었습니다. (타임아웃)", flush=True)
    os._exit(1) 

def progress_hook(d):
    global timer
    
    if timer:
        timer.cancel()
    
    if d['status'] == 'downloading':
        total = d.get('total_bytes') or d.get('total_bytes_estimate') or 0
        downloaded = d.get('downloaded_bytes', 0)
        if total > 0:
            percent = (downloaded / total) * 100
            print(f"PROGRESS:{percent:.1f}", end="\n", flush=True)
            
        timer = threading.Timer(120.0, timeout_handler)
        timer.start()
        
    elif d['status'] == 'finished':
        print("PROGRESS:100.0", end="\n", flush=True)
        timer = None

def download_video(url, out_path, resolution):
    global timer
    
    ydl_opts = {
        'progress_hooks': [progress_hook],
        'quiet': True,
        'noprogress': True,
        'no_cache_dir': True,              # 👈 1. 캐시 폴더 사용 안 함 (에러 방지)
        'ignoreerrors': True,
    }

    # 💡 [핵심 추가] target_res가 mp3일 경우 음악 추출용 옵션 세팅
    if resolution.lower() == "mp3":
        if out_path.endswith('.mp3'):
            base_path = out_path[:-4]
        else:
            base_path = out_path

        ydl_opts.update({
            'format': 'bestaudio/best',  # 최고 음질의 오디오 스트림 선택
            'outtmpl': f"{base_path}.%(ext)s",
            'audio_ext': 'mp3',          # 오디오 확장자를 mp3로 고정 유도
            'postprocessors': [{         # ffmpeg를 이용한 MP3 오디오 변환 후처리 필터
                'key': 'FFmpegExtractAudio',
                'preferredcodec': 'mp3',
                'preferredquality': '0', # 0은 자동 최고음질 (또는 '192', '320' 지정 가능)
            }],
        })
    else:
        # 화질별 포맷 스트링 설정
        if resolution == "1080p":
            fmt_str = "bv[height<=1080]+ba/bestvideo[height<=1080]+bestaudio/best"
        elif resolution == "720p":
            fmt_str = "bv[height<=720]+ba/bestvideo[height<=720]+bestaudio/best"
        elif resolution == "480p":
            fmt_str = "bv[height<=480]+ba/bestvideo[height<=480]+bestaudio/best"
        else:
            fmt_str = "bv+ba/bestvideo+bestaudio/best"
            
        ydl_opts.update({
            'format': fmt_str,
            'outtmpl': out_path,
            'merge_output_format': 'mp4',
            
            # 💡 [핵심 해결책] 
            # 임시 파일(temp.mp4)을 만들 때 오류를 내는 주원인인 오디오 코덱(Opus)을
            # MP4 전용 표준 코덱인 'aac'로 강제 변환(재인코딩)하면서 병합하도록 지시합니다.
            'postprocessor_args': {
                'mergedvideo': [
                    '-c:v', 'copy',     # 비디오는 손실 없이 고속 복사
                    '-c:a', 'aac',      # 오디오는 무조건 aac로 변환하여 충돌 원천 차단
                ]
            },
            'fixup': 'detect_or_warn',
        })
    
    # ⏱️ 메타데이터 추출 시작 전 120초 타이머 작동
    timer = threading.Timer(120.0, timeout_handler)
    timer.start()
    
    class DownloadStopException(Exception): pass

    try:
        with yt_dlp.YoutubeDL(ydl_opts) as ydl:
            try:
                info = ydl.extract_info(url, download=False)
                url_type = info.get('_type')
                
                if (url_type == 'playlist') or ('entries' in info):
                    print("PLAYLIST:플레이리스트는 다운로드하지 못합니다.", flush=True)
                    raise DownloadStopException()
                
                # 5시간보다 긴 영상은 다운로드하지 못하도록 제한
                duration = info.get('duration', 0)
                if duration > 18000:
                    print("ERROR:영상 길이가 5시간을 초과하여 다운로드할 수 없습니다.", flush=True)
                    raise DownloadStopException()

                meta = {
                    "title": info.get("title", "Unknown Title"),
                    "thumbnail": info.get("thumbnail", "")
                }
                print(f"METADATA:{json.dumps(meta)}", end="\n", flush=True)

                # 💡 다운로드 전에 미리 예상 최종 파일명을 뱉어줍니다.
                actual_filename = os.path.basename(ydl.prepare_filename(info))
                if resolution.lower() == "mp3" and not actual_filename.endswith('.mp3'):
                    actual_filename = os.path.splitext(actual_filename)[0] + '.mp3'
                print(f"SUCCESS:{actual_filename}", flush=True)
                
            except DownloadStopException:
                if timer: timer.cancel()
                sys.exit(1)
            except Exception as e:
                if timer: timer.cancel()
                error_msg = str(e)
                if "timeout" in error_msg.lower() or "timed out" in error_msg.lower():
                    print("ERROR:유튜브 서버와의 연결 시간이 초과되었습니다. (타임아웃)", flush=True)
                else:
                    print(f"ERROR:영상 정보를 가져오지 못했습니다. 원인: {error_msg}", flush=True)
                sys.exit(1)

            # 실제 다운로드 및 변환 실행
            try:
                ydl.download([url])
                
            except Exception as e:
                if timer: timer.cancel()
                error_msg = str(e)
                if "timeout" in error_msg.lower() or "timed out" in error_msg.lower():
                    print("ERROR:다운로드 중 유튜브 서버와의 연결이 끊어졌습니다. (타임아웃)", flush=True)
                else:
                    print(f"ERROR:다운로드 진행 중 에러 발생: {error_msg}", flush=True)
                sys.exit(1)
                
    finally:
        if timer:
            timer.cancel()

if __name__ == "__main__":
    if len(sys.argv) < 4:
        print("ERROR:인자 부족 - 사용법: downloader.py <유튜브 URL> <출력 경로> <화질/mp3>", flush=True)
        sys.exit(1)
        
    video_url = sys.argv[1]
    output_path = sys.argv[2]
    target_res = sys.argv[3]
    
    timer = None
    
    try:
        download_video(video_url, output_path, target_res)
    except Exception as e:
        if timer:
            timer.cancel()
        print(f"ERROR:{str(e)}", flush=True)
        sys.exit(1)