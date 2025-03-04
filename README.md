# ociv - Interactive TUI OCI layout inspector

Run it with the name of a directory that has OCI layouts somewhere under it. It
will display all layouts and images in the entire tree.

```bash
ociv .
```

![image](https://github.com/project-machine/oci-viewer/assets/1768106/c9ffd36c-1f4b-4acf-824f-38ae856f9e6b)

use the `j,k,n,p` keys or the mouse to move through the tree in the left pane.

Control-S highlights the search panel which updates the tree with matches as
you type. The first match will be selected automatically. Hit enter to refocus
on the tree and select other matches.

Control-Q exits.

## layer contents display

ociv since 1.7.1 will show a subtree of the layers in each image, and selecting a layer will show the actual contents of the layer blob on the summary pane.

Note this is currently just the straight file listing of the archives, with whiteouts included. No attempt is made to compare to other layers and/or show information about what might be hidden by whiteouts, etc.

<img width="1694" alt="image" src="https://github.com/user-attachments/assets/d2b62c89-90f8-4197-af65-8ab37cae5f3c" />


## known layer name display

ociv will look for a file called `known-layers.json` in `$HOME/.cache/ociv/`
(or `~$SUDO_USER/.cache/ociv/` if running as root, e.g. to look at an OCI layout
in a mounted squashfs filesystem) to annotate any displayed layer hashes with
the tags in that file, so as to identify base images easily.

The format of that file is a list of `Name,Hash` string pairs like this:

```json
[{"Name": "c3/bird:1.0.56-squashfs", "Hash": "e6539655d80241d1a43ea9b00ba2e56b3cccd2a55027c21ad44f359cded63dea"},
 {"Name": "c3/bird:1.0.56", "Hash": "8b54d9ceaa3d8a957e4dcb1c7ff96eb4e39bdd8847a1e0752ef7c0b4f6128b36"},
 {"Name": "c3/bird:1.0.57-squashfs", "Hash": "0254746330bb206cc49589b25eb6c4d45430b502ff4318f6bb1225e602a40358"}
]
```

The included python script `get-published-layers.py` can be used to generate
this file from a registry, or update an existing file when new sets of images
are published, for example:

```bash
./get-published-layers.py http://my.registry.tld/v2 myrepoprefix ./known-layers.json
```

## Summary of base images used in all images in a directory

If you select a directory or a layout, the display shows summary info about all
the images in there, and what base images are or are not in common between them.

TODO: public screenshot needed

## Shows OCI Artifacts, Referrers and Notary Signatures

![image](https://github.com/project-machine/oci-viewer/assets/1768106/8b374ce1-e1ec-4179-9497-a064cb373711)
