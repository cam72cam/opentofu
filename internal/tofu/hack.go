package tofu

import "os"

func useNewEngine() bool {
	return os.Getenv("TOFU_ENGINE") == "new"
}
