# Call Graph DOT Export

`erst debug --export-dot <tx-hash>` writes a Graphviz-compatible DOT file next to the debug output.

## Generate the DOT file

```bash
erst debug --export-dot 5c0a1234567890abcdef1234567890abcdef1234567890abcdef1234567890ab
```

This creates:

```text
5c0a1234567890abcdef1234567890abcdef1234567890abcdef1234567890ab.callgraph.dot
```

Cross-contract edges are labeled with:

- `gas`: estimated gas cost derived from the simulated transaction budget
- `calls`: how many times that caller invoked that callee

## Visualize with Graphviz

Render to SVG:

```bash
dot -Tsvg 5c0a1234567890abcdef1234567890abcdef1234567890abcdef1234567890ab.callgraph.dot -o callgraph.svg
```

Render large graphs with a force-directed layout:

```bash
sfdp -Tpng 5c0a1234567890abcdef1234567890abcdef1234567890abcdef1234567890ab.callgraph.dot -o callgraph.png
```
