' Keeps dedicated Command Code switcher running in background for T3 Code on port 8766
Set shell = CreateObject("WScript.Shell")
exe = "C:\Users\Beambe\Projects\commandcode-account-switcher\bin\cmd-switch.exe"

serverRunning = False
On Error Resume Next
Set http = CreateObject("MSXML2.ServerXMLHTTP")
http.Open "GET", "http://127.0.0.1:8766/api/status", False
http.Send
If Err.Number = 0 Then
    If http.Status = 200 Then
        serverRunning = True
    End If
End If
On Error GoTo 0

If serverRunning Then
    WScript.Quit
End If

Do
    shell.Run """" & exe & """ serve", 0, True
    WScript.Sleep 5000
Loop
