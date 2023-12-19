package slowinit

import "time"

func init() {
	time.Sleep(time.Second*5)
	print("slowinit done\n")
}