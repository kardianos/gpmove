package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kardianos/task"
	"gopkg.in/yaml.v2"
)

/*
Step 1: Read all existing file descriptions (yml) in the sidecar folder.
Step 2: Index the OriginalName, note the UID and file location.
Step 3: Read each Google Photos JSON file "title" field
Step 4: Match with OriginalName (no ext) and move JSON file to the Original folder next to he image.
Step 5: Manually re-index the images.



---sidecar yml file---
data@kardianos1 /d/r/p/s/s/2/01> cat 20180101_000253_2C6CF514.yml
TakenAt: 2018-01-01T00:02:53Z
TakenSrc: meta
UID: pqnkwgd18ghabopp
Type: image
Title: Beaverton / USA / 2018
OriginalName: IMG_20171231_160253871
PlaceSrc: meta
Altitude: 25
Lat: 45.448994
Lng: -122.796295
Year: 2017
Month: 12
Day: 31
ISO: 640
Exposure: 1/24
FNumber: 1.7
FocalLength: 4
CameraSerial: ZY224JR2TL
Details:
  Keywords: beaverton, gold, google, oregon, usa
CreatedAt: 2021-01-27T05:55:25.001738034Z
UpdatedAt: 2021-01-27T05:55:26.606953386Z


--- google photos json file ---
{
  "title": "VID_20200516_132742175.mp4",
  ...
}

---
Final google photos json file should be:
<photo filename base>.json
20181220_092609_94FBD9E6.jpg
20181220_092609_94FBD9E6.json
*/
func main() {
	err := task.Start(context.Background(), 2*time.Second, run)
	if err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	cmd := &task.Command{
		Commands: []*task.Command{
			{
				Name: "movejson",
				Flags: []*task.Flag{
					{Name: "import", Default: "", Usage: "Import Folder"},
					{Name: "sidecar", Default: "", Usage: "Sidecar Folder"},
					{Name: "original", Default: "", Usage: "Original Folder"},
				},
				Action: task.ActionFunc(func(ctx context.Context, st *task.State, sc task.Script) error {
					im := st.Get("import").(string)
					sidecar := st.Get("sidecar").(string)
					original := st.Get("original").(string)

					wr := os.Stderr
					logf := func(f string, v ...interface{}) {
						fmt.Fprintf(wr, f, v...)
					}

					lookup := make(map[string]parts, 10000)

					sidecarCleaner, err := newPathCleaner(sidecar)
					if err != nil {
						return err
					}
					err = filepath.Walk(sidecar, func(p string, info os.FileInfo, err error) error {
						if info.IsDir() {
							return nil
						}
						if err != nil {
							return err
						}
						ext := filepath.Ext(p)
						if ext != ".yml" {
							return nil
						}
						logf("sidecar found %s\n", p)
						scParts, err := sidecarCleaner.Split(p)
						if err != nil {
							return err
						}
						f, err := os.Open(p)
						if err != nil {
							return err
						}
						defer f.Close()

						coder := yaml.NewDecoder(f)
						type sidecar struct {
							OriginalName string `yaml:"OriginalName"`
						}
						sc := sidecar{}
						err = coder.Decode(&sc)
						if err != nil {
							return err
						}
						logf("\tlog %q as %+v", sc.OriginalName, scParts)
						if len(sc.OriginalName) == 0 {
							logf(" skip\n")
							return nil
						}
						logf(".\n")

						lookup[sc.OriginalName] = scParts
						return nil
					})
					if err != nil {
						return err
					}
					err = filepath.Walk(im, func(p string, info os.FileInfo, err error) error {
						if info.IsDir() {
							return nil
						}
						if err != nil {
							return err
						}
						ext := filepath.Ext(p)
						if ext != ".json" {
							return nil
						}
						logf("json found %s\n", p)

						f, err := os.Open(p)
						if err != nil {
							return err
						}
						defer f.Close()

						coder := json.NewDecoder(f)
						type googlePhoto struct {
							Title string `json:"title"`
						}
						gp := googlePhoto{}
						err = coder.Decode(&gp)
						if err != nil {
							return err
						}
						logf("\tTitle: %s\n", gp.Title)

						titleExt := filepath.Ext(gp.Title)
						base := gp.Title[:len(gp.Title)-len(titleExt)]
						logf("\tBase: %s\n", base)
						parts, ok := lookup[base]
						if !ok {
							return nil
						}

						moveTo := filepath.Join(original, parts.Path, parts.Base+".json")

						_, err = os.Stat(moveTo)
						exists := !os.IsNotExist(err)
						logf("\tMove To (exists: %v): %s\n", exists, moveTo)
						if exists {
							return nil
						}
						err = os.Rename(p, moveTo)
						if err != nil {
							return err
						}
						return nil
					})
					if err != nil {
						return err
					}
					return nil
				}),
			},
		},
		{
			Name: "alignjson",
			Flags: []*task.Flag{
				{Name: "original", Default: "", Usage: "Original Folder"},
			},
			Action: task.ActionFunc(func(ctx context.Context, st *task.State, sc task.Script) error {
				original := st.Get("original").(string)

				wr := os.Stderr
				logf := func(f string, v ...interface{}) {
					fmt.Fprintf(wr, f, v...)
				}

				origCleaner, err := newPathCleaner(original)
				if err != nil {
					return err
				}
				err = filepath.Walk(original, func(p string, info os.FileInfo, err error) error {
					if info.IsDir() {
						return nil
					}
					if err != nil {
						return err
					}
					ext := filepath.Ext(p)
					if ext != ".json" {
						return nil
					}
					logf("json found %s\n", p)
					scParts, err := origCleaner.Split(p)
					if err != nil {
						return err
					}
					

					return nil
				})
				if err != nil {
					return err
				}
				return nil
			}),
		},
	},
	}

	st := task.DefaultState()
	return task.Run(ctx, st, cmd.Exec(os.Args[1:]))
}

type pathCleaner struct {
	root string
}

func newPathCleaner(root string) (*pathCleaner, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	return &pathCleaner{
		root: root,
	}, nil
}

type parts struct {
	Path string
	Base string
}

func (pc *pathCleaner) Split(p string) (parts, error) {
	pt := parts{}
	p, err := filepath.Abs(p)
	if err != nil {
		return pt, err
	}
	if !strings.HasPrefix(p, pc.root) {
		return pt, fmt.Errorf("path %q not found in root %q", p, pc.root)
	}
	rel := strings.TrimPrefix(p[len(pc.root):], "/")
	ext := filepath.Ext(rel)
	rel = rel[:len(rel)-len(ext)]
	dir, base := filepath.Split(rel)
	pt = parts{
		Path: dir,
		Base: base,
	}
	return pt, nil
}
