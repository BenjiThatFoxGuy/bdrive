package banner

import (
	"fmt"
	"strings"
)

const banner = `
 ____  ____       _
| __ )|  _ \ _ __(_)_   _____
|  _ \| | | | '__| \ \ / / _ \
| |_) | |_| | |  | |\ V /  __/
|____/|____/|_|  |_| \_/ \___|

`

type StartupInfo struct {
	Version  string
	Addr     string
	LogLevel string
}

func PrintBanner(info StartupInfo) {
	fmt.Print(banner)
	fmt.Printf("                        v%s\n\n", info.Version)

	width := 50
	fmt.Printf("  %s\n", strings.Repeat("─", width))
	fmt.Printf("  → Address:   http://%s\n", formatAddr(info.Addr))
	fmt.Printf("  → Log Level: %s\n", info.LogLevel)
	fmt.Printf("  %s\n", strings.Repeat("─", width))
	fmt.Println()
}

func formatAddr(addr string) string {
	if strings.HasPrefix(addr, ":") {
		return "localhost" + addr
	}
	return addr
}
