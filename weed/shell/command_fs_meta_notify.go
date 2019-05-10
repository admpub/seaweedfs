package shell

import (
	"context"
	"fmt"
	"io"

	"github.com/chrislusf/seaweedfs/weed/filer2"
	"github.com/chrislusf/seaweedfs/weed/notification"
	"github.com/chrislusf/seaweedfs/weed/pb/filer_pb"
	weed_server "github.com/chrislusf/seaweedfs/weed/server"
	"github.com/spf13/viper"
)

func init() {
	commands = append(commands, &commandFsMetaNotify{})
}

type commandFsMetaNotify struct {
}

func (c *commandFsMetaNotify) Name() string {
	return "fs.meta.notify"
}

func (c *commandFsMetaNotify) Help() string {
	return `recursively send directory and file meta data to notifiction message queue

	fs.meta.notify	# send meta data from current directory to notification message queue

	The message queue will use it to trigger replication from this filer.

`
}

func (c *commandFsMetaNotify) Do(args []string, commandEnv *commandEnv, writer io.Writer) (err error) {

	filerServer, filerPort, path, err := commandEnv.parseUrl(findInputDirectory(args))
	if err != nil {
		return err
	}

	weed_server.LoadConfiguration("notification", true)
	v := viper.GetViper()
	notification.LoadConfiguration(v.Sub("notification"))

	ctx := context.Background()

	return commandEnv.withFilerClient(ctx, filerServer, filerPort, func(client filer_pb.SeaweedFilerClient) error {

		var dirCount, fileCount uint64

		err = doTraverse(ctx, writer, client, filer2.FullPath(path), func(parentPath filer2.FullPath, entry *filer_pb.Entry) error {

			if entry.IsDirectory {
				dirCount++
			} else {
				fileCount++
			}

			return notification.Queue.SendMessage(
				string(parentPath.Child(entry.Name)),
				&filer_pb.EventNotification{
					NewEntry: entry,
				},
			)

		})

		if err == nil {
			fmt.Fprintf(writer, "\ntotal notified %d directories, %d files\n", dirCount, fileCount)
		}

		return err

	})

}
