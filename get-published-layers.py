#!/usr/bin/env python3

from datetime import date
import argparse
import json
import requests
import shutil
import sys
try:
    import progressbar
except:
    print("requires progressbar, pip3 install progressbar please")
    sys.exit(1)


def getInfoMatchingPrefixes(regAPIURL, prefixes, existingImages):
    infos = []
    r = requests.get(f'{regAPIURL}/_catalog')
    repolist = r.json()
    repos = repolist['repositories']
    matchingrepos = [r for r in repos if any([r.startswith(p) for p in prefixes])]
    bar = progressbar.ProgressBar(prefix='{variables.task} >> {variables.subtask}',
                                  variables={'task': '--', 'subtask': '--'}).start()

    for repo in matchingrepos:

        tr = requests.get(f'{regAPIURL}/{repo}/tags/list')
        tagd = tr.json()

        imagename = tagd['name']
        taglist = tagd['tags']
        for tag in taglist:
            if f'{imagename}:{tag}' in existingImages:
                continue
            if tag.startswith('commit'):
                continue
            rm = requests.get(f'{regAPIURL}/{repo}/manifests/{tag}')
            manifest = rm.json()
            layers = manifest['layers']
            lastlayer = layers[-1]
            lastlayerhash = lastlayer['digest'][7:]
            infos.append(dict(Name=f'{imagename}:{tag}', Hash=f'{lastlayerhash}'))
            bar.update(bar.value + 1, task=repo, subtask=tag)
    return infos


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description='update layers file')
    parser.add_argument('registryapiurl', help="url to image registry api endpoint")
    parser.add_argument('prefixes')
    parser.add_argument('inputfile')

    args = parser.parse_args()

    prefixes = args.prefixes.split(',')
    existingImages = set()
    cur = []
    with open(args.inputfile, 'r') as inf:
        cur = json.load(inf)
        existingImages = set([e['Name'] for e in cur])

    layerInfos = getInfoMatchingPrefixes(args.registryapiurl, prefixes, existingImages)
    print(f'updating existing {len(cur)} with {len(layerInfos)} new hashes')
    layerInfos += cur
    outfilename = args.inputfile+'-'+date.today().isoformat()
    shutil.copyfile(args.inputfile, outfilename)

    with open(args.inputfile, 'w') as f:
        json.dump(layerInfos, f)

    print(f'done. saved previous data to {outfilename}, updated {args.inputfile} inline')
