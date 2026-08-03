import type { BTFile } from './types'

export type FileTreeNode = {
  name: string
  path: string
  isDir: boolean
  file?: BTFile
  children: FileTreeNode[]
}

export function buildFileTree(files: BTFile[]): FileTreeNode {
  const root: FileTreeNode = { name: '', path: '', isDir: true, children: [] }
  for (const file of files) {
    const parts = file.path.replace(/\\/g, '/').split('/').filter(Boolean)
    let node = root
    let prefix = ''
    for (let index = 0; index < parts.length; index += 1) {
      const part = parts[index]
      prefix = prefix ? `${prefix}/${part}` : part
      const isLast = index === parts.length - 1
      if (isLast) {
        node.children.push({
          name: part,
          path: file.path.replace(/\\/g, '/'),
          isDir: false,
          file,
          children: [],
        })
        continue
      }
      let child = node.children.find((entry) => entry.isDir && entry.name === part)
      if (!child) {
        child = { name: part, path: prefix, isDir: true, children: [] }
        node.children.push(child)
      }
      node = child
    }
  }
  sortTree(root)
  return root
}

function sortTree(node: FileTreeNode) {
  node.children.sort((left, right) => {
    if (left.isDir !== right.isDir) return left.isDir ? -1 : 1
    return left.name.localeCompare(right.name, undefined, { sensitivity: 'base' })
  })
  for (const child of node.children) {
    if (child.isDir) sortTree(child)
  }
}

export function collectFileIndexes(node: FileTreeNode): number[] {
  if (!node.isDir) {
    return node.file ? [node.file.index] : []
  }
  return node.children.flatMap((child) => collectFileIndexes(child))
}

export function defaultExpandedPaths(root: FileTreeNode): Set<string> {
  const expanded = new Set<string>()
  function walk(node: FileTreeNode, depth: number) {
    if (!node.isDir) return
    if (node.path) expanded.add(node.path)
    // Expand first two levels by default for readability.
    if (depth < 2) {
      for (const child of node.children) walk(child, depth + 1)
    }
  }
  for (const child of root.children) walk(child, 0)
  return expanded
}
