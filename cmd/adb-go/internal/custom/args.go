package custom

import "flag"

// splitFlagSetCommand returns the first remaining non-flag argument as the
// subcommand and every later non-flag argument as that command's arguments.
func splitFlagSetCommand(fs *flag.FlagSet) (string, []string, bool) {
	if fs.NArg() == 0 {
		return "", nil, false
	}
	command := fs.Arg(0)
	commandArgs := make([]string, 0, fs.NArg()-1)
	for i := 1; i < fs.NArg(); i++ {
		commandArgs = append(commandArgs, fs.Arg(i))
	}
	return command, commandArgs, true
}
