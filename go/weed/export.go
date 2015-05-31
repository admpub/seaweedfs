package main

import (
	"archive/tar"
	"bytes"
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/chrislusf/seaweedfs/go/glog"
	"github.com/chrislusf/seaweedfs/go/storage"
)

func init() {
	cmdExport.Run = runExport // break init cycle
}

const (
	defaultFnFormat = `{{.Mime}}/{{.Id}}:{{.Name}}`
	timeFormat      = "2006-01-02T15:04:05"
)

var cmdExport = &Command{
	UsageLine: "export -dir=/tmp -volumeId=234 -o=/dir/name.tar -fileNameFormat={{.Name}} -newer='" + timeFormat + "'",
	Short:     "list or export files from one volume data file",
	Long: `List all files in a volume, or Export all files in a volume to a tar file if the output is specified.

	The format of file name in the tar file can be customized. Default is {{.Mime}}/{{.Id}}:{{.Name}}. Also available is {{.Key}}.

  `,
}

var (
	exportVolumePath = cmdExport.Flag.String("dir", ".", "input data directory to store volume data files")
	exportCollection = cmdExport.Flag.String("collection", "", "the volume collection name")
	exportVolumeId   = cmdExport.Flag.Int("volumeId", -1, "a volume id. The volume .dat and .idx files should already exist in the dir.")
	dest             = cmdExport.Flag.String("o", "", "output tar file name, must ends with .tar, or just a \"-\" for stdout")
	format           = cmdExport.Flag.String("fileNameFormat", defaultFnFormat, "filename format, default to {{.Mime}}/{{.Id}}:{{.Name}}")
	newer            = cmdExport.Flag.String("newer", "", "export only files newer than this time, default is all files. Must be specified in RFC3339 without timezone")
	tarFh            *tar.Writer
	tarHeader        tar.Header
	fnTmpl           *template.Template
	fnTmplBuf        = bytes.NewBuffer(nil)
	newerThan        time.Time
	newerThanUnix    int64 = -1
	localLocation, _       = time.LoadLocation("Local")
)

func runExport(cmd *Command, args []string) bool {

	var err error

	if *newer != "" {
		if newerThan, err = time.ParseInLocation(timeFormat, *newer, localLocation); err != nil {
			fmt.Println("cannot parse 'newer' argument: " + err.Error())
			return false
		}
		newerThanUnix = newerThan.Unix()
	}

	if *exportVolumeId == -1 {
		return false
	}

	if *dest != "" {
		if *dest != "-" && !strings.HasSuffix(*dest, ".tar") {
			fmt.Println("the output file", *dest, "should be '-' or end with .tar")
			return false
		}

		if fnTmpl, err = template.New("name").Parse(*format); err != nil {
			fmt.Println("cannot parse format " + *format + ": " + err.Error())
			return false
		}

		var fh *os.File
		if *dest == "-" {
			fh = os.Stdout
		} else {
			if fh, err = os.Create(*dest); err != nil {
				glog.Fatalf("cannot open output tar %s: %s", *dest, err)
			}
		}
		defer fh.Close()
		tarFh = tar.NewWriter(fh)
		defer tarFh.Close()
		t := time.Now()
		tarHeader = tar.Header{Mode: 0644,
			ModTime: t, Uid: os.Getuid(), Gid: os.Getgid(),
			Typeflag:   tar.TypeReg,
			AccessTime: t, ChangeTime: t}
	}

	fileName := strconv.Itoa(*exportVolumeId)
	if *exportCollection != "" {
		fileName = *exportCollection + "_" + fileName
	}
	vid := storage.VolumeId(*exportVolumeId)
	indexFile, err := os.OpenFile(path.Join(*exportVolumePath, fileName+".idx"), os.O_RDONLY, 0644)
	if err != nil {
		glog.Fatalf("Create Volume Index [ERROR] %s\n", err)
	}
	defer indexFile.Close()

	nm, err := storage.LoadNeedleMap(indexFile)
	if err != nil {
		glog.Fatalf("cannot load needle map from %s: %s", indexFile.Name(), err)
	}

	var version storage.Version

	err = storage.ScanVolumeFile(*exportVolumePath, *exportCollection, vid,
		storage.NeedleMapInMemory,
		func(superBlock storage.SuperBlock) error {
			version = superBlock.Version()
			return nil
		}, true, func(n *storage.Needle, offset int64) error {
			nv, ok := nm.Get(n.Id)
			glog.V(3).Infof("key %d offset %d size %d disk_size %d gzip %v ok %v nv %+v",
				n.Id, offset, n.Size, n.DiskSize(), n.IsGzipped(), ok, nv)
			if ok && nv.Size > 0 && int64(nv.Offset)*8 == offset {
				if newerThanUnix >= 0 && n.HasLastModifiedDate() && n.LastModified < uint64(newerThanUnix) {
					glog.V(3).Infof("Skipping this file, as it's old enough: LastModified %d vs %d",
						n.LastModified, newerThanUnix)
					return nil
				}
				return walker(vid, n, version)
			}
			if !ok {
				glog.V(2).Infof("This seems deleted %d size %d", n.Id, n.Size)
			} else {
				glog.V(2).Infof("Skipping later-updated Id %d size %d", n.Id, n.Size)
			}
			return nil
		})
	if err != nil {
		glog.Fatalf("Export Volume File [ERROR] %s\n", err)
	}
	return true
}

type nameParams struct {
	Name string
	Id   uint64
	Mime string
	Key  string
}

func walker(vid storage.VolumeId, n *storage.Needle, version storage.Version) (err error) {
	key := storage.NewFileIdFromNeedle(vid, n).String()
	if tarFh != nil {
		fnTmplBuf.Reset()
		if err = fnTmpl.Execute(fnTmplBuf,
			nameParams{Name: string(n.Name),
				Id:   n.Id,
				Mime: string(n.Mime),
				Key:  key,
			},
		); err != nil {
			return err
		}
		nm := fnTmplBuf.String()

		if n.IsGzipped() && path.Ext(nm) != ".gz" {
			nm = nm + ".gz"
		}

		tarHeader.Name, tarHeader.Size = nm, int64(len(n.Data))
		if n.HasLastModifiedDate() {
			tarHeader.ModTime = time.Unix(int64(n.LastModified), 0)
		} else {
			tarHeader.ModTime = time.Unix(0, 0)
		}
		tarHeader.ChangeTime = tarHeader.ModTime
		if err = tarFh.WriteHeader(&tarHeader); err != nil {
			return err
		}
		_, err = tarFh.Write(n.Data)
	} else {
		size := n.DataSize
		if version == storage.Version1 {
			size = n.Size
		}
		fmt.Printf("key=%s Name=%s Size=%d gzip=%t mime=%s\n",
			key,
			n.Name,
			size,
			n.IsGzipped(),
			n.Mime,
		)
	}
	return
}
