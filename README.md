# DJMAX Audio Watcher

Windows에서 **DJMAX RESPECT V**가 실행되면 기본 출력 장치의 샘플레이트를 **48 kHz**로 변경하고, 게임이 종료되면 **실행 전 출력 형식**으로 복구하는 트레이 유틸리티입니다.

> **v5 (테스트 버전)** — Windows 10/11 x64용. Windows 실기기 실행 테스트는 아직 진행하지 않았습니다. **게임 실행 시 브라우저 등의 소리가 항상 끊기지 않는다고 보장하지는 않습니다.** 오디오 형식 변경에 대한 기존 앱의 반응은 Windows/드라이버/앱에 따라 달라집니다.

## 기능

- DJMAX 실행: **24-bit / 48,000 Hz / 스테레오** 우선 요청. 실제 장치가 32-bit / 48 kHz로 보고되더라도 48 kHz 스테레오면 성공으로 처리
- **이미 48 kHz 스테레오면 설정 명령을 생략**해 불필요한 오디오 장치 재초기화 최소화
- 24-bit 요청 후 48 kHz 적용에 실패한 경우에만 32-bit, 16-bit 순서로 시도
- DJMAX 종료: 저장한 원래 출력 형식으로 복구. **이미 원래 형식이면 복구 명령 생략**
- 게임 시작은 WMI 이벤트, 종료는 Windows 프로세스 핸들 종료 신호를 사용. **상시 프로세스 목록 폴링 없음**
- Windows 트레이 아이콘에서 상태 확인 / 로그인 자동 실행 변경 / 로그 열기 / 종료
- 수동 실행 시 `예: 자동 실행` / `아니요: 이번만 실행(자동 실행 해제)` / `취소` 선택
- 로그인 자동 실행(`--startup`) 시에는 선택 창 표시 안 함
- 기존 오디오 형식 백업은 복구 검증에 성공할 때까지 보존
- **v2~v4가 실행 중이면 v5 실행을 막아** 서로 충돌하는 상황을 예방

## 설치 및 실행

1. 기존 v2/v3/v4를 **정상 종료**합니다. v4는 트레이 아이콘 우클릭 → **종료**.
2. **GitHub Releases 통합 배포 ZIP**에는 검증된 **SVCL 1.28 x64 공식 배포 파일 전체**가 함께 포함됩니다. 소스만 빌드한 경우 `powershell -ExecutionPolicy Bypass -File .\install_svcl.ps1`로 공식 배포본 전체를 다운로드·검증하여 EXE와 같은 폴더에 설치할 수 있습니다.
3. `DJMAX_Audio_Watcher_v5.exe`를 실행하고 Windows 관리자 권한 요청을 승인합니다.
4. 수동 실행 선택 창에서 원하는 자동 시작 옵션을 고릅니다.
5. Windows 작업 표시줄 알림 영역의 아이콘을 우클릭하여 상태와 설정을 관리합니다. 아이콘이 안 보이면 `^` 숨겨진 아이콘을 확인하세요.

> **외부 프로그램:** 소스 저장소에는 `svcl.exe`를 커밋하지 않습니다. 무료 통합 배포 ZIP을 생성할 때 공식 **SVCL 1.28 x64 배포본 전체를 수정 없이 포함**합니다. NirSoft의 별도 재배포 조건을 반드시 준수해야 합니다. 자세한 내용은 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)를 참고하세요.

## 빌드

Go **1.23 이상**이 필요합니다. 추가 Go 모듈 의존성은 없습니다.

```powershell
# Windows PowerShell, 저장소 루트에서 실행
.\build.ps1
```

또는:

```powershell
go build -trimpath -ldflags="-H=windowsgui -s -w" -o DJMAX_Audio_Watcher_v5.exe .
```

테스트 (Windows):

```powershell
go test ./...
```

GitHub Actions에서도 Windows 빌드·테스트를 수행하고, 공식 SVCL 1.28 x64 전체 패키지를 SHA-256으로 검증해 **무료 통합 배포 ZIP**을 만듭니다.

```powershell
# Windows에서 통합 배포 ZIP 생성 (공식 NirSoft 사이트 접근 필요)
.\package-release.ps1
# 결과: release/DJMAX_Audio_Watcher_v5_Windows_x64_SVCL_1.28.zip
```

**SVCL 버전 고정:** 검증된 `svcl.exe` SHA-256은 `0c4738296d253495bc995da7b5938e27b306c8f204dc9f7d4d5d455f1d8d3e38`입니다. 공식 ZIP의 SHA-256도 확인하며, 내용이 달라지면 **빌드를 중단**합니다. 이는 오디오 끊김이 절대 없다는 보장은 아니지만, SVCL 버전 차이로 인한 변동을 방지합니다.

## 작동 방식

1. 시작 전 현재 출력 형식을 `%LOCALAPPDATA%\DJMAXAudioWatcher\original-format.dat`에 백업합니다.
2. `svcl.exe /SaveDeviceFormat`으로 **현재 형식을 먼저 조회**합니다. 이미 48 kHz 스테레오라면 `/SetDefaultFormat`을 호출하지 않습니다.
3. 변경이 필요한 경우 `/SetDefaultFormat DefaultRenderDevice 24 48000 2`를 실행하고, **실제 적용 형식**을 다시 확인합니다. 확인에 실패하면 다른 비트 깊이를 시도합니다.
4. 게임 프로세스 종료 신호를 받으면 저장된 원본 형식과 현재 형식을 비교합니다. 같다면 변경하지 않고 백업만 정리합니다. 다르면 `/LoadDeviceFormat`으로 복구 후 검증합니다.

> 비트 수보다 **실제로 48 kHz 스테레오가 적용되었는지**를 우선 검증합니다. 장치 드라이버가 24비트 요청을 32비트 PCM으로 보고하는 경우가 있습니다.

## 로그 및 문제 해결

- 로그: `%LOCALAPPDATA%\DJMAXAudioWatcher\watcher.log`
- 백업: `%LOCALAPPDATA%\DJMAXAudioWatcher\original-format.dat`
- `SKIP SetDefaultFormat`: 이미 48 kHz라서 불필요한 전환을 생략함
- `SKIP LoadDeviceFormat`: 이미 원래 형식이라서 불필요한 복구를 생략함
- `48kHz verified`: 48 kHz 스테레오 적용 확인
- `original format restored and verified`: 원래 출력 형식 복구 확인
- `ERROR`: 실패한 명령 및 복구 검증 내역 확인

**주의:** 게임 실행 중 감시기를 종료하면 게임 종료를 감지할 수 없으므로 즉시 자동 복구되지 않습니다. 백업은 보존되고 다음 실행 시 게임이 종료된 상태라면 복구를 시도합니다. 문제가 생기면 Windows 사운드 설정에서 출력 형식을 직접 되돌릴 수 있습니다.

Windows 출력 형식 변경으로 인해 **Zen/Firefox 등의 기존 오디오 스트림이 무음 상태가 될 수 있습니다.** 불필요한 설정 명령을 줄였지만, 최초로 192 kHz → 48 kHz를 변경하는 상황에서의 무음까지 막는 기능은 아닙니다.

## 자동 시작과 업데이트

- `예`를 선택하면 Windows 작업 스케줄러에 로그인 시 관리자 권한으로 실행하는 작업을 등록합니다.
- `아니요`를 선택하면 기존 자동 시작 작업을 해제하고 이번만 실행합니다.
- v2/v3/v4와 같은 작업 이름(`DJMAXAudioWatcherV2`)을 사용하여 **기존 예약 작업의 실행 경로를 v5로 갱신**합니다.
- v5를 다른 폴더로 옮긴 경우 수동 실행 후 `예`를 선택하여 작업 경로를 갱신하세요.
- 관리자 권한 PowerShell에서 `./DJMAX_Audio_Watcher_v5.exe --uninstall`을 실행하면 자동 시작 작업을 해제할 수 있습니다. 실행 중이라면 먼저 트레이에서 종료하세요.

## 프로젝트 구성

```text
.
├── main.go                  # 감시, 오디오 형식 전환/복구, Windows 작업 스케줄러
├── tray_windows.go          # 시스템 트레이 UI
├── main_test.go             # 기본 형식 파싱/전환 판정 테스트
├── go.mod
├── build.ps1               # Windows 빌드 스크립트
├── install_svcl.ps1        # 공식 SVCL 1.28 전체 배포본 다운로드/해시 검증
├── package-release.ps1     # Watcher + 공식 SVCL 통합 ZIP 생성
├── THIRD_PARTY_NOTICES.md  # NirSoft 저작권/재배포 조건 및 해시
├── .gitignore              # EXE, svcl.exe, 로그/백업 제외
└── .github/workflows/build.yml
```

## 라이선스

이 프로젝트의 공개 라이선스는 아직 지정하지 않았습니다. GitHub 공개 전에 MIT 등 원하는 라이선스를 검토하고 `LICENSE` 파일을 추가하세요. 외부 유틸리티인 SoundVolumeCommandLine의 라이선스는 별도이며 **무료·비상업적 재배포와 원본 배포 패키지 전체의 무수정 포함** 조건이 있습니다. 통합 ZIP을 판매하거나 상용 제품 일부로 배포하지 마세요.
