package main

import (
	"github.com/fsouza/go-dockerclient"

	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
)

type Image struct {
	Id          string
	ParentId    string   `json:",omitempty"`
	RepoTags    []string `json:",omitempty"`
	VirtualSize int64
	Size        int64
	Created     int64
}

type ImagesCommand struct {
	Dot        bool `short:"d" long:"dot" description:"Show image information as Graphviz dot."`
	Tree       bool `short:"t" long:"tree" description:"Show image information as tree."`
	Short      bool `short:"s" long:"short" description:"Show short summary of images (repo name and list of tags)."`
	NoTruncate bool `short:"n" long:"no-trunc" description:"Don't truncate the image IDs."`
}

var imagesCommand ImagesCommand

func (x *ImagesCommand) Execute(args []string) error {

	var images *[]Image

	stat, err := os.Stdin.Stat()
	if err != nil {
		return fmt.Errorf("error reading stdin stat", err)
	}

	if (stat.Mode() & os.ModeCharDevice) == 0 {
		// read in stdin
		stdin, err := ioutil.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("error reading all input", err)
		}

		images, err = parseImagesJSON(stdin)
		if err != nil {
			return err
		}

	} else {

		client, err := connect()
		if err != nil {
			return err
		}

		clientImages, err := client.ListImages(docker.ListImagesOptions{All: true})
		if err != nil {
			if in_docker := os.Getenv("IN_DOCKER"); len(in_docker) > 0 {
				return fmt.Errorf("Unable to access Docker socket, please run like this:\n  docker run --rm -v /var/run/docker.sock:/var/run/docker.sock nate/dockviz images <args>\nFor more help, run 'dockviz help'")
			} else {
				return fmt.Errorf("Unable to connect: %s\nFor help, run 'dockviz help'", err)
			}
		}

		var ims []Image
		for _, image := range clientImages {
			// fmt.Println(image)
			ims = append(ims, Image{
				image.ID,
				image.ParentID,
				image.RepoTags,
				image.VirtualSize,
				image.Size,
				image.Created,
			})
		}

		images = &ims
	}

	if imagesCommand.Dot {
		fmt.Printf(jsonToDot(images))
	} else if imagesCommand.Tree {

		var startImage = ""
		if len(args) > 0 {

			// attempt to find the start image, which can be specified as an
			// image ID or a repository name

			startImageArg := args[0]
			startImageRepo := args[0]

			// in case a repo name was specified, append ":latest" if it isn't
			// already there
			if !strings.HasSuffix(startImageRepo, ":latest") {
				startImageRepo = fmt.Sprintf("%s:latest", startImageRepo)
			}

		IMAGES:
			for _, image := range *images {
				// check if the start image arg matches an image id
				if strings.Index(image.Id, startImageArg) == 0 {
					startImage = startImageArg
					break IMAGES
				}

				// check if the start image arg matches an repository name
				if image.RepoTags[0] != "<none>:<none>" {
					for _, repotag := range image.RepoTags {
						if repotag == startImageRepo {
							startImage = image.Id
							break IMAGES
						}
					}
				}
			}

			if startImage == "" {
				return fmt.Errorf("Unable to find image %s.", startImageArg)
			}
		}

		fmt.Printf(jsonToTree(images, startImage, imagesCommand.NoTruncate))
	} else if imagesCommand.Short {
		fmt.Printf(jsonToShort(images))
	} else {
		return fmt.Errorf("Please specify either --dot, --tree, or --short")
	}

	return nil
}

func jsonToTree(images *[]Image, startImageArg string, noTrunc bool) string {
	var buffer bytes.Buffer

	var startImage Image

	var roots []Image
	var byParent = make(map[string][]Image)
	for _, image := range *images {
		if image.ParentId == "" {
			roots = append(roots, image)
		} else {
			if children, exists := byParent[image.ParentId]; exists {
				byParent[image.ParentId] = append(children, image)
			} else {
				byParent[image.ParentId] = []Image{image}
			}
		}

		if startImageArg != "" {
			if startImageArg == image.Id || startImageArg == truncate(image.Id) {
				startImage = image
			}

			for _, repotag := range image.RepoTags {
				if repotag == startImageArg {
					startImage = image
				}
			}
		}
	}

	if startImageArg != "" {
		WalkTree(&buffer, noTrunc, []Image{startImage}, byParent, "")
	} else {
		WalkTree(&buffer, noTrunc, roots, byParent, "")
	}

	return buffer.String()
}

func WalkTree(buffer *bytes.Buffer, noTrunc bool, images []Image, byParent map[string][]Image, prefix string) {
	if len(images) > 1 {
		length := len(images)
		for index, image := range images {
			if index+1 == length {
				PrintTreeNode(buffer, noTrunc, image, prefix+"└─")
				if subimages, exists := byParent[image.Id]; exists {
					WalkTree(buffer, noTrunc, subimages, byParent, prefix+"  ")
				}
			} else {
				PrintTreeNode(buffer, noTrunc, image, prefix+"├─")
				if subimages, exists := byParent[image.Id]; exists {
					WalkTree(buffer, noTrunc, subimages, byParent, prefix+"│ ")
				}
			}
		}
	} else {
		for _, image := range images {
			PrintTreeNode(buffer, noTrunc, image, prefix+"└─")
			if subimages, exists := byParent[image.Id]; exists {
				WalkTree(buffer, noTrunc, subimages, byParent, prefix+"  ")
			}
		}
	}
}

func PrintTreeNode(buffer *bytes.Buffer, noTrunc bool, image Image, prefix string) {
	var imageID string
	if noTrunc {
		imageID = image.Id
	} else {
		imageID = truncate(image.Id)
	}

	buffer.WriteString(fmt.Sprintf("%s%s Virtual Size: %s", prefix, imageID, humanSize(image.VirtualSize)))
	if image.RepoTags[0] != "<none>:<none>" {
		buffer.WriteString(fmt.Sprintf(" Tags: %s\n", strings.Join(image.RepoTags, ", ")))
	} else {
		buffer.WriteString(fmt.Sprintf("\n"))
	}
}

func humanSize(raw int64) string {
	sizes := []string{"B", "KB", "MB", "GB", "TB"}

	rawFloat := float64(raw)
	ind := 0

	for {
		if rawFloat < 1000 {
			break
		} else {
			rawFloat = rawFloat / 1000
			ind = ind + 1
		}
	}

	return fmt.Sprintf("%.01f %s", rawFloat, sizes[ind])
}

func truncate(id string) string {
	return id[0:12]
}

func parseImagesJSON(rawJSON []byte) (*[]Image, error) {

	var images []Image
	err := json.Unmarshal(rawJSON, &images)

	if err != nil {
		return nil, fmt.Errorf("Error reading JSON: ", err)
	}

	return &images, nil
}

func jsonToDot(images *[]Image) string {

	var buffer bytes.Buffer
	buffer.WriteString("digraph docker {\n")

	for _, image := range *images {
		if image.ParentId == "" {
			buffer.WriteString(fmt.Sprintf(" base -> \"%s\" [style=invis]\n", truncate(image.Id)))
		} else {
			buffer.WriteString(fmt.Sprintf(" \"%s\" -> \"%s\"\n", truncate(image.ParentId), truncate(image.Id)))
		}
		if image.RepoTags[0] != "<none>:<none>" {
			buffer.WriteString(fmt.Sprintf(" \"%s\" [label=\"%s\\n%s\",shape=box,fillcolor=\"paleturquoise\",style=\"filled,rounded\"];\n", truncate(image.Id), truncate(image.Id), strings.Join(image.RepoTags, "\\n")))
		}
	}

	buffer.WriteString(" base [style=invisible]\n}\n")

	return buffer.String()
}

func jsonToShort(images *[]Image) string {
	var buffer bytes.Buffer

	var byRepo = make(map[string][]string)

	for _, image := range *images {
		for _, repotag := range image.RepoTags {
			if repotag != "<none>:<none>" {

				// parse the repo name and tag name out
				// tag is after the last colon
				lastColonIndex := strings.LastIndex(repotag, ":")
				tagname := repotag[lastColonIndex+1:]
				reponame := repotag[0:lastColonIndex]

				if tags, exists := byRepo[reponame]; exists {
					byRepo[reponame] = append(tags, tagname)
				} else {
					byRepo[reponame] = []string{tagname}
				}
			}
		}
	}

	for repo, tags := range byRepo {
		buffer.WriteString(fmt.Sprintf("%s: %s\n", repo, strings.Join(tags, ", ")))
	}

	return buffer.String()
}

func init() {
	parser.AddCommand("images",
		"Visualize docker images.",
		"",
		&imagesCommand)
}
