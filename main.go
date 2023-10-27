package main

import (
	"bytes"
	"context"

	"fmt"
	"io/ioutil"
	"log"
	"os"

	"path/filepath"
	"sort"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/olekukonko/tablewriter"

	ispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/opencontainers/umoci"

	"github.com/rivo/tview"
	"github.com/urfave/cli/v2"
)

func isNodeOCILayout(node *tview.TreeNode) bool {
	reference := node.GetReference()
	if reference == nil {
		return false
	}
	if ref, ok := reference.(treeInfo); ok {
		return isOCILayout(ref.path)
	}
	return false
}

func addContentsOfLayout(target *tview.TreeNode, path string) ([]imageInfo, []subIndexInfo) {
	imageInfos := []imageInfo{}
	subIndexInfos := []subIndexInfo{}
	oci, err := umoci.OpenLayout(path)

	if err != nil {
		log.Printf("error opening layout at path '%s': %w", path, err)
		return imageInfos, subIndexInfos
	}
	defer oci.Close()

	index, err := oci.GetIndex(context.Background())
	if err != nil {
		log.Printf("error getting index in path %s: %v", path, err)
		return imageInfos, subIndexInfos
	}

	// get info for every image/artifact/subindex in the layout, regardless of whether or
	// not they are tagged:
	for _, descriptor := range index.Manifests {
		dgst := descriptor.Digest.String()[7:]
		newref := imageref{
			layoutpath: path,
			hash:       dgst,
		}

		tag, ok := descriptor.Annotations[ispec.AnnotationRefName]
		if ok {
			newref.tag = tag
		}

		switch descriptor.MediaType {
		case ispec.MediaTypeImageManifest:
			imageInfo := loadImageManifest(oci, newref, descriptor)
			ImageInfoMap[newref.hash] = imageInfo
			log.Printf("appending imageinfo for newref=%+v", newref)
			imageInfos = append(imageInfos, imageInfo)
		case ispec.MediaTypeImageIndex:
			subref := subIndexRef{
				hash:       dgst,
				layoutpath: path,
				tag:        newref.tag,
			}
			subInfo := loadSubIndexManifest(oci, subref, descriptor)
			SubIndexInfoMap[subref.hash] = subInfo
			subIndexInfos = append(subIndexInfos, subInfo)
		default:
			log.Printf("Don't know what to do with a top level descriptor like this: %+v", descriptor)
		}

	}

	for _, imageInfo := range imageInfos {

		log.Printf("image %q digest is %v\n", imageInfo.displayName, imageInfo.manifestDescriptor.Digest)
		node := tview.NewTreeNode(imageInfo.displayLabel).
			SetReference(imageInfo.ref).
			SetSelectable(true)
		referrers, err := getReferrersForImage(oci, path, &imageInfo.manifestDescriptor)
		if err != nil {
			log.Printf("error getting referrers:%w\n", err)
		}

		for _, referrerDescriptor := range referrers.Manifests {
			log.Printf("referrer: %+v\n", referrerDescriptor)

			dgst := referrerDescriptor.Digest.String()[7:]
			referrerImageInfo := ImageInfoMap[dgst]
			referrerImageInfo.ref.targetTag = imageInfo.ref.tag
			referrerImageInfo.ref.targetHash = imageInfo.ref.hash

			refNode := tview.NewTreeNode(referrerImageInfo.displayLabel).
				SetReference(referrerImageInfo.ref).
				SetSelectable(true)

			node.AddChild(refNode)
		}
		target.AddChild(node)
		log.Printf("    done loading image %q", imageInfo.displayName)
	}

	for _, subIndexInfo := range subIndexInfos {
		node := tview.NewTreeNode(subIndexInfo.displayLabel).
			SetReference(subIndexInfo.ref).
			SetSelectable(true)

		for _, desc := range subIndexInfo.manifestDescriptors {
			dgst := desc.Digest.String()[7:]
			subIndexedImageInfo := ImageInfoMap[dgst]

			subIndexedNode := tview.NewTreeNode(subIndexedImageInfo.displayLabel).
				SetReference(subIndexedImageInfo.ref).
				SetSelectable(true)
			node.AddChild(subIndexedNode)
		}
		target.AddChild(node)
	}

	return imageInfos, subIndexInfos
}

type treeInfo struct {
	path          string
	numLayouts    int
	imageInfos    []imageInfo
	subIndexInfos []subIndexInfo
}

func (ti *treeInfo) update(other treeInfo) {
	ti.numLayouts += other.numLayouts
	ti.imageInfos = append(ti.imageInfos, other.imageInfos...)
	// TODO: should we ignore path, should this be a tree structure
}

type summaryItem struct {
	digest7 string
	names   []string
	count   int
	users   string
}
type byCount []summaryItem

func (a byCount) Len() int      { return len(a) }
func (a byCount) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a byCount) Less(i, j int) bool {
	if a[i].count == a[j].count {
		return a[i].digest7 < a[j].digest7
	}
	return a[i].count < a[j].count
}

func getNamesOfSelfOrUniqueDescendantLayer(digest string, allLayers map[string][]string) []string {
	return getNamesOfSelfOrUniqueDescendantLayerWithLogIndent(digest, allLayers, 0)
}
func getNamesOfSelfOrUniqueDescendantLayerWithLogIndent(digest string, allLayers map[string][]string, logindent int) []string {

	ilog := func(fmtstr string, indent int, args ...interface{}) {}
	/*
		    debugilog := func(fmtstr string, indent int, args ...interface{}) {
				indentstr := ""
				for i := 0; i < indent; i++ {
					indentstr += "  "
				}
				fmtstr = fmt.Sprintf("%s%s", indentstr, fmtstr)
				log.Printf(fmtstr, args...)
			}
	*/
	ilog("getNamesOfSelf for %q", logindent, digest)

	names := getNamesForHash(digest)
	// if there are names for digest, return them
	ilog("names is %v", logindent, names)

	if len(names) > 1 {
		ilog("just returning names %v", logindent, names)
		return names
	}
	if len(names) == 1 && names[0] != "?" {
		ilog("returning names with an asterisk %v", logindent, names)
		return []string{fmt.Sprintf("%v*", names[0])}
	}

	children := allLayers[digest]
	ilog("children is %v", logindent, children)
	// if there is only one child, recurse on it, get the first name we can
	if len(children) == 1 {
		return getNamesOfSelfOrUniqueDescendantLayerWithLogIndent(children[0], allLayers, logindent+1)
	}

	// if there are 0 or multiple children, just return digest, there is no unique descendant:

	ilog(" returning just names %v", logindent, names)
	return names

}

func (ti *treeInfo) summary() string {
	s := fmt.Sprintf("%s: %d layouts, %d images\n\nbase image info:\n (base images marked with a * are not the first layer, just the first named layer)\n", ti.path, ti.numLayouts, len(ti.imageInfos))

	allInternalKnownLayersStr := "\n\nAll known tags used internally in these images:\n"
	allInternalKnownLayersSet := make(map[string][]string)
	allLayers := make(map[string][]string)

	baseLayerMap := map[string][]string{}

	// builds:
	// allInternalKnownLayersSet, a map of known layer names to lists of images that have those layers anywhere in their stack
	// allLayers, an adjacency list of hashes representing the tree of all layers
	// baseLayerMap, a map of image tags to the initial base layer in the image stack
	for _, info := range ti.imageInfos {
		pathAndTag := fmt.Sprintf("%s/%s", filepath.Base(info.ref.layoutpath), info.ref.tag)

		baseLayer := info.layerDigests[0]
		baseLayerMap[baseLayer] = append(baseLayerMap[baseLayer], pathAndTag)
		for idx, digest := range info.layerDigests {
			if idx == len(info.layerDigests)-1 {
				// ignore last layer for the internalknownlayersset, it is not "internal"
				continue
			} else {
				nextLayer := info.layerDigests[idx+1]
				currentChildren := allLayers[digest]
				nextLayerAlreadyInChildren := false
				for _, child := range currentChildren {
					if nextLayer == child {
						nextLayerAlreadyInChildren = true

					}
				}
				if !nextLayerAlreadyInChildren {
					allLayers[digest] = append(allLayers[digest], nextLayer)
				}
			}
			for _, name := range getNamesForHash(digest) {
				if name == "?" {
					continue
				}
				allInternalKnownLayersSet[name] = append(allInternalKnownLayersSet[name], pathAndTag)
			}
		}
	}

	var uniqueInternalKnownLayers []string
	for internalKnownLayer := range allInternalKnownLayersSet {
		uniqueInternalKnownLayers = append(uniqueInternalKnownLayers, internalKnownLayer)
	}

	if len(uniqueInternalKnownLayers) == 0 {
		allInternalKnownLayersStr = "\nNo known layer tags detected in internal layers in images in this layout."
	} else {
		sort.Strings(uniqueInternalKnownLayers)

		for _, layer := range uniqueInternalKnownLayers {
			users := allInternalKnownLayersSet[layer]
			allInternalKnownLayersStr += layer + " in " + strings.Join(users, ", ") + "\n\n"
		}
	}
	buf := new(bytes.Buffer)
	tw := tablewriter.NewWriter(buf)
	tw.SetHeader([]string{"digest7", "base layer names", "number of uses", "images using that base"})
	tw.SetColWidth(100)
	tw.SetBorder(false)
	tw.SetColumnSeparator(" ")
	summaryItems := []summaryItem{}
	for baseHash, users := range baseLayerMap {
		digest := baseHash
		if len(baseHash) > 7 {
			digest = baseHash[:7]

		}

		usersString := ""
		for idx, user := range users {
			usersString += user
			if idx < len(users)-1 {
				usersString += ", "
			}
		}

		names := getNamesOfSelfOrUniqueDescendantLayer(baseHash, allLayers)

		summaryItems = append(summaryItems,
			summaryItem{
				digest7: digest,
				names:   getShortStringForNames(names),
				users:   usersString,
				count:   len(users),
			})
	}
	sort.Sort(sort.Reverse(byCount(summaryItems)))
	for _, item := range summaryItems {
		tw.Append([]string{
			item.digest7,
			strings.Join(item.names, ","),
			fmt.Sprintf("%d", item.count),
			item.users})

	}
	tw.Render()

	return s + buf.String() + allInternalKnownLayersStr
}

func addOCILayoutNodes(target *tview.TreeNode, root string, needle string) treeInfo {
	node := tview.NewTreeNode("placeholder").
		SetSelectable(true)

	if isOCILayout(root) {
		imageInfos, subIndexInfos := addContentsOfLayout(node, root)
		node.SetText(fmt.Sprintf("%s (%d images)", filepath.Base(root), len(imageInfos)))
		target.AddChild(node)
		ti := treeInfo{
			path:          root,
			numLayouts:    1,
			imageInfos:    imageInfos,
			subIndexInfos: subIndexInfos,
		}
		node.SetReference(ti)
		return ti
	}

	paths, err := ioutil.ReadDir(root)
	if err != nil {
		panic(err)
	}

	thisTreeInfo := treeInfo{
		path: root,
	}

	for _, path := range paths {
		if !path.IsDir() {
			continue
		}
		fullPath := filepath.Join(root, path.Name())
		pathTreeInfo := addOCILayoutNodes(node, fullPath, needle)
		thisTreeInfo.update(pathTreeInfo)
	}
	if thisTreeInfo.numLayouts > 0 {
		node.SetText(fmt.Sprintf("%s (%d layouts)", filepath.Base(root), thisTreeInfo.numLayouts))
		target.AddChild(node)
	}
	node.SetReference(thisTreeInfo)
	return thisTreeInfo

}

func main() {

	app := &cli.App{
		Name:      "ociv",
		Usage:     "interactively inspect oci layouts",
		ArgsUsage: "root dirs to inspect",
		Action:    doTViewStuff,
	}

	file, err := os.OpenFile("log.txt", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		log.Fatal(err)
	}

	log.SetOutput(file)

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}

func getAllChildren(node *tview.TreeNode) []*tview.TreeNode {
	var children []*tview.TreeNode
	for _, child := range node.GetChildren() {
		children = append(children, child)
		children = append(children, getAllChildren(child)...)
	}
	return children
}

func getMatchingTreeNodes(node *tview.TreeNode, needle string) []*tview.TreeNode {
	reference := node.GetReference()
	thisNodeMatches := false

	// reference nil is the root, doesn't match anything
	if reference != nil {
		var haystacks []string
		switch ref := reference.(type) {
		case treeInfo:
			haystacks = []string{ref.path}

		case imageref:
			haystacks = reference.(imageref).searchString()

		case subIndexRef:
			haystacks = []string{}
			info := SubIndexInfoMap[ref.hash]
			for _, manifestDesc := range info.manifestDescriptors {
				haystacks = append(haystacks, manifestDesc.Digest.String()[7:])
			}

		default:
			log.Printf("unknown type for reference %v: %T", reference, reference)
		}

		for _, haystack := range haystacks {
			if strings.Contains(haystack, needle) {
				thisNodeMatches = true
			}
		}
	}

	if thisNodeMatches {
		ret := []*tview.TreeNode{node}
		allChildren := getAllChildren(node)
		return append(ret, allChildren...)
	}

	// if this node doesn't match, still return it if a child matches:
	children := node.GetChildren()
	var childMatches []*tview.TreeNode
	if len(children) > 0 {
		for _, child := range children {
			childMatches = append(childMatches, getMatchingTreeNodes(child, needle)...)
		}
	}

	if len(childMatches) > 0 {
		ret := []*tview.TreeNode{node}
		return append(ret, childMatches...)
	}

	return []*tview.TreeNode{}
}

func clearTreeFormatting(node *tview.TreeNode, selectable bool) {

	if selectable {

		reference := node.GetReference()
		if reference != nil {

			switch reference.(type) {
			case treeInfo:
				node.SetColor(tcell.ColorBlue)
			case imageref:
				node.SetColor(tcell.ColorRed)
			case subIndexRef:
				node.SetColor(tcell.ColorBlue)
			default:
				log.Printf("unknown type for reference %v: %T", reference, reference)
			}
		}

	} else {
		node.SetColor(tcell.ColorGray)

	}
	node.SetSelectable(selectable)
	children := node.GetChildren()
	if len(children) > 0 {
		for _, child := range children {
			clearTreeFormatting(child, selectable)
		}
	}
}

func doTViewStuff(ctxt *cli.Context) error {

	setupWellKnownLayerNames()

	rootDirs := ctxt.Args().Slice()
	log.Print(rootDirs)
	if len(rootDirs) == 0 {
		rootDirs = []string{"."}
	}

	app := tview.NewApplication()

	root := tview.NewTreeNode("Your forest of OCI layouts").
		SetColor(tcell.ColorRed)

	tree := tview.NewTreeView().
		SetRoot(root).
		SetCurrentNode(root).SetAlign(false).SetTopLevel(1).SetGraphics(true)
	tree.Box.SetBorder(true)

	searchInputField := tview.NewInputField().
		SetLabel("Search: ").
		SetChangedFunc(func(needle string) {

			if needle == "" {
				tree.SetCurrentNode(nil)
				clearTreeFormatting(root, true)
				return
			}

			// look through all tree children and highlight ones that match, and
			// autoselect the first match that is an oci layout

			matches := getMatchingTreeNodes(root, needle)
			if len(matches) == 0 {
				clearTreeFormatting(root, true)
			} else {
				// set everything unselectable to allow us to just set the matches selectable
				clearTreeFormatting(root, false)

				firstLayoutNodeIdx := 0

				for idx, match := range matches {
					if isNodeOCILayout(match) && firstLayoutNodeIdx == 0 {
						firstLayoutNodeIdx = idx
					}
					match.SetColor(tcell.ColorYellow)
					match.SetSelectable(true)
				}

				tree.SetCurrentNode(matches[firstLayoutNodeIdx])
				// force a process() call
				tree.Move(1)
				tree.Move(-1)

			}
			// update info pane with summaries
		})
	searchInputField.SetDoneFunc(func(key tcell.Key) {
		app.SetFocus(tree)
	})

	searchInputField.Box.SetBorder(true)

	treeGrid := tview.NewGrid().SetRows(0, 3).SetColumns(0).
		AddItem(tree, 0, 0, 1, 1, 0, 0, true).
		AddItem(searchInputField, 1, 0, 1, 1, 0, 0, false)

	summaries := []string{}
	for _, rootDir := range rootDirs {
		treeInfo := addOCILayoutNodes(root, rootDir, "")
		summaries = append(summaries, tview.Escape(treeInfo.summary()))
	}
	clearTreeFormatting(root, true)
	infoPane := tview.NewTextView().
		SetTextAlign(tview.AlignLeft).
		SetText(strings.Join(summaries, "\n")).
		SetDynamicColors(true).
		SetRegions(true)
	infoPane.Box.SetBorder(true)

	statusLine := tview.NewTextView().
		SetTextAlign(tview.AlignCenter).
		SetText("press 'ctrl-q' to exit, 'ctrl-s' to search")

	selfunc := func(node *tview.TreeNode) {
		reference := node.GetReference()
		if reference == nil {
			infoPane.SetText(strings.Join(summaries, "\n"))
			infoPane.ScrollToBeginning()
			return
		}
		children := node.GetChildren()
		if len(children) == 0 {
			switch ref := reference.(type) {
			case imageref:
				infoPane.SetText(ref.summary())
				infoPane.ScrollToBeginning()
			case treeInfo:
				infoPane.SetText(tview.Escape(ref.summary()))
			case subIndexRef:
				infoPane.SetText(ref.summary())
			default:
				log.Printf("node ref is unknown type: %T\n", reference)
			}
		} else {
			switch ref := reference.(type) {
			case imageref:
				infoPane.SetText(ref.summary())
				infoPane.ScrollToBeginning()
			case treeInfo:
				infoPane.SetText(ref.summary())
				infoPane.ScrollToBeginning()
			case subIndexRef:
				infoPane.SetText(ref.summary())
			default:
				log.Printf("node ref is unknown type: %T\n", reference)
				infoPane.SetText("error")
			}
		}
		return

	}
	tree.SetSelectedFunc(selfunc)
	tree.SetChangedFunc(selfunc)

	// customise the movement keys to auto select instead of waiting for space or enter
	tree.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch key := event.Key(); key {
		case tcell.KeyRune:
			if r := event.Rune(); key == tcell.KeyRune {
				switch r {
				case 'k':
					fallthrough
				case 'p':
					tree.Move(-1)
				case 'j':
					fallthrough
				case 'n':
					tree.Move(1)
				case 'r':
					infoPane.Clear()
				}
				cur := tree.GetCurrentNode()
				if cur != nil {
					tree.SetCurrentNode(cur)
					selfunc(cur)
				}
				return nil
			}
		case tcell.KeyEnter:
			// enter toggles expanded setting
			cur := tree.GetCurrentNode()
			if cur != nil {
				cur.SetExpanded(!cur.IsExpanded())
			}
			return nil
		}
		return event
	})

	mainGrid := tview.NewGrid().
		SetRows(0, 1).
		SetColumns(-1, -3).
		AddItem(treeGrid, 0, 0, 1, 1, 0, 0, true).
		AddItem(infoPane, 0, 1, 1, 1, 0, 0, false).
		AddItem(statusLine, 1, 0, 1, 2, 0, 0, false)

	tabbableViews := []tview.Primitive{tree, searchInputField, infoPane}
	tabbableViewIdx := 0

	setNewFocusedViewIdx := func(prevIdx int, newIdx int) {
		//		prev := tabbableViews[prevIdx%len(tabbableViews)]
		//		 note: remember to check if prev == new
		new := tabbableViews[newIdx%len(tabbableViews)]
		app.SetFocus(new)
	}

	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch key := event.Key(); key {
		// case tcell.KeyRune:
		// 	if r := event.Rune(); key == tcell.KeyRune && r == 'q' {
		// 		app.Stop()
		// 		return nil
		// 	}
		case tcell.KeyCtrlQ:
			app.Stop()
		case tcell.KeyCtrlS:
			prev := tabbableViewIdx
			tabbableViewIdx := 1
			setNewFocusedViewIdx(prev, tabbableViewIdx)
		case tcell.KeyTab:
			prev := tabbableViewIdx
			tabbableViewIdx += 1
			setNewFocusedViewIdx(prev, tabbableViewIdx)
		case tcell.KeyBacktab:
			prev := tabbableViewIdx
			tabbableViewIdx -= 1
			setNewFocusedViewIdx(prev, tabbableViewIdx)
		}
		return event
	})

	if err := app.SetRoot(mainGrid, true).EnableMouse(true).Run(); err != nil {
		panic(err)
	}
	return nil
}
