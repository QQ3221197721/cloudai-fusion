param([Parameter(Mandatory=$true)][string[]]$Files)
$enc = New-Object System.Text.UTF8Encoding($false)
foreach ($f in $Files) {
    $text = [System.IO.File]::ReadAllText($f)
    # Remove a leading //go:build ignore line (and optional following blank line)
    $new = [System.Text.RegularExpressions.Regex]::Replace($text, "^//go:build ignore\r?\n(\r?\n)?", "")
    if ($new -ne $text) {
        [System.IO.File]::WriteAllText($f, $new, $enc)
        Write-Output "STRIPPED: $f"
    } else {
        Write-Output "NOCHANGE: $f"
    }
}
