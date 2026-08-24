package main
import (
	"fmt"
	"github.com/mattn/go-runewidth"
)
func main() {
	name := "报告——最终版.md"
	fmt.Printf("name=%q runewidth=%d\n", name, runewidth.StringWidth(name))
	fmt.Printf("timeFmt=%q width=%d\n", "2006-01-02 15:04", runewidth.StringWidth("2006-01-02 15:04"))
	fmt.Printf("1.5 KB width=%d\n", runewidth.StringWidth("1.5 KB"))
	fmt.Printf("TOTAL fixed = %d (mark 2 + sp 1 + size 12 + sp 1 + time 16)\n", 2+1+12+1+16)
}
