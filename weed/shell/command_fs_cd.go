package shell

import (
	"context"
	"io"
)

func init() {
	commands = append(commands, &commandFsCd{})
}

type commandFsCd struct {
}

func (c *commandFsCd) Name() string {
	return "fs.cd"
}

func (c *commandFsCd) Help() string {
	return `change directory to http://<filer_server>:<port>/dir/

	The full path can be too long to type. For example,
		fs.ls http://<filer_server>:<port>/some/path/to/file_name

	can be simplified as

		fs.cd http://<filer_server>:<port>/some/path
		fs.ls to/file_name
`
}

func (c *commandFsCd) Do(args []string, commandEnv *commandEnv, writer io.Writer) (err error) {

	input := findInputDirectory(args)

	filerServer, filerPort, path, err := commandEnv.parseUrl(input)
	if err != nil {
		return err
	}

	if path == "/" {
		commandEnv.option.FilerHost = filerServer
		commandEnv.option.FilerPort = filerPort
		commandEnv.option.Directory = "/"
		return nil
	}

	ctx := context.Background()

	err = commandEnv.checkDirectory(ctx, filerServer, filerPort, path)

	if err == nil {
		commandEnv.option.FilerHost = filerServer
		commandEnv.option.FilerPort = filerPort
		commandEnv.option.Directory = path
	}

	return err
}
