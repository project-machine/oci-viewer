# ociv
Interactive TUI OCI layout inspector.

Run it with the name of a directory that has OCI layouts somewhere under it. It will search the entire tree.

```
ociv .
```




use the `j,k,n,p` keys or the mouse to move through the tree in the left pane.

Control-S highlights the search panel which updates the tree with matches as you type.
The first match will be selected automatically. Hit enter to refocus on the tree and select other matches.

Control-Q exits.

## known layer name display

ociv will look for a file called `known-layers.json` in `$HOME/.cache/ociv/` (
or `~$SUDO_USER/.cache/ociv/` if running as root, e.g. to look at an OCI layout
in a mounted squashfs filesystem) to annotate any displayed layer hashes with
the tags in that file, so as to identify base images easily.

The format of that file is a list of `Name,Hash` string pairs like this:

``` json
[{"Name": "c3/bird:1.0.56-squashfs", "Hash": "e6539655d80241d1a43ea9b00ba2e56b3cccd2a55027c21ad44f359cded63dea"},
 {"Name": "c3/bird:1.0.56", "Hash": "8b54d9ceaa3d8a957e4dcb1c7ff96eb4e39bdd8847a1e0752ef7c0b4f6128b36"},
 {"Name": "c3/bird:1.0.57-squashfs", "Hash": "0254746330bb206cc49589b25eb6c4d45430b502ff4318f6bb1225e602a40358"}
]
```

The included python script `get-published-layers.py` can be used to generate
this file from a registry, or update an existing file when new sets of images
are published.

## summary of base images used in all images in a directory 

If you select a directory or a layout, the display shows summary info about all the images in there:



## Shows OCI Artifacts, Referrers and Notary Signatures



