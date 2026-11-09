Set WshShell = CreateObject("WScript.Shell")
Set appShell = CreateObject("Shell.Application")

' Start Antigravity switcher on 8765
WshShell.Run "wscript ""C:\Users\Beambe\Projects\antigravity-account-switcher\scripts\start_t3_switcher.vbs""", 0, False

' Start Command Code switcher on 8766
WshShell.Run "wscript ""C:\Users\Beambe\Projects\commandcode-account-switcher\scripts\start_commandcode_switcher.vbs""", 0, False

' Open T3 Code IDE
WshShell.Run """C:\Users\Beambe\AppData\Local\Programs\t3code\T3 Code (Beta).exe""", 1, False
