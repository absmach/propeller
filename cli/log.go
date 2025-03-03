package cli

import (
	"encoding/json"
	"fmt"

	"github.com/fatih/color"
	"github.com/hokaccha/go-prettyjson"
	"github.com/spf13/cobra"
)

func logJSONCmd(cmd cobra.Command, iList ...interface{}) {
	for _, i := range iList {
		m, err := json.Marshal(i)
		if err != nil {
			logErrorCmd(cmd, err)

			return
		}

		pj, err := prettyjson.Format(m)
		if err != nil {
			logErrorCmd(cmd, err)

			return
		}

		fmt.Fprintf(cmd.OutOrStdout(), "\n%s\n\n", string(pj))
	}
}

func logUsageCmd(cmd cobra.Command, u string) {
	fmt.Fprintf(cmd.OutOrStdout(), color.YellowString("\nusage: %s\n\n"), u)
}

func logErrorCmd(cmd cobra.Command, err error) {
	boldRed := color.New(color.FgRed, color.Bold)
	boldRed.Fprintf(cmd.ErrOrStderr(), "\nerror: ")

	fmt.Fprintf(cmd.ErrOrStderr(), "%s\n\n", color.RedString(err.Error()))
}

func logOKCmd(cmd cobra.Command) {
	fmt.Fprintf(cmd.OutOrStdout(), "\n%s\n\n", color.BlueString("ok"))
}

func logSuccessCmd(cmd cobra.Command, msg string) {
	fmt.Fprintf(cmd.OutOrStdout(), "\n%s\n", color.GreenString(msg))
}
