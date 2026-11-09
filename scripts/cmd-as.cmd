@echo off
setlocal
if "%~1"=="" (
  echo Usage: cmd-as ^<alias^> [args...]
  echo Example: cmd-as acc2
  exit /b 1
)

set ALIAS=%~1
shift

set VAULT_AUTH=%USERPROFILE%.commandcodeaccounts%ALIAS%auth.json
if not exist "%VAULT_AUTH%" (
  echo Error: Account '%ALIAS%' not found in vault (%VAULT_AUTH%^).
  echo Run 'cmd-switch list-accounts' to view registered accounts.
  exit /b 1
)

for /f "tokens=2 delims=:," %%i in ('findstr /i "apiKey" "%VAULT_AUTH%"') do (
  set KEY=%%i
)
set KEY=%KEY: =%
set KEY=%KEY:"=%

set COMMAND_CODE_API_KEY=%KEY%
echo [cmd-as] Running Command Code with account '%ALIAS%'...
call command-code %*
