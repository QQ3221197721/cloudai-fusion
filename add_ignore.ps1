param(
    [Parameter(Mandatory=$true)][string[]]$Files,
    [string]$Note = ""
)
$enc = New-Object System.Text.UTF8Encoding($false)
foreach ($f in $Files) {
    $text = [System.IO.File]::ReadAllText($f)
    if ($text -match "^//go:build ignore") {
        Write-Output "ALREADY-IGNORED: $f"
        continue
    }
    $header = "//go:build ignore`r`n"
    if ($Note -ne "") { $header += "// NOTE: $Note`r`n" }
    $header += "`r`n"
    [System.IO.File]::WriteAllText($f, $header + $text, $enc)
    Write-Output "RE-IGNORED: $f"
}
