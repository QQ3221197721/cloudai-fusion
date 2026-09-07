param([Parameter(Mandatory=$true)][string[]]$Files,[string]$Note="")
$enc = New-Object System.Text.UTF8Encoding($false)
foreach ($f in $Files) {
    $text = [System.IO.File]::ReadAllText($f)
    if ($text -match "^//go:build ignore") { Write-Output "ALREADY: $f"; continue }
    $h = "//go:build ignore`r`n"
    if ($Note -ne "") { $h += "// NOTE: $Note`r`n" }
    $h += "`r`n"
    [System.IO.File]::WriteAllText($f, $h + $text, $enc)
    Write-Output "RE-IGNORED: $f"
}
