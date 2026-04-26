## WQF7003 Intelligent Computation

## Graph and Tree - A Revision

## Graphs

- Graph → Network
- Informally a graph is a set of nodes joined by a set of lines or arrows.
- Representing a problem as a graph can make a problem much simpler.

<!-- image -->

<!-- image -->

## Application of Graphs

- In real world applications, graph is the base for all kinds of network.

## Business ties in Us biotechindustry

<!-- image -->

Oceltech Friendship Network

<!-- image -->

## Application of Graphs in Computer Science

## Internet

## State Space Analysis

<!-- image -->

## What makes a problem graph-like?

- There are two components to a graph
- Vertex/Nodes
- Edges
- In graph-like problems, these components have natural correspondences to problem elements
- Entities are nodes and interactions between entities are edges
- Many complex systems are graph-like

## Definition

- A graph (specifically, undirected graph) G:
- consists of a set V of vertices, and
- a set E of edges
- each edge e E is associated with an ∈ unordered pair of vertices.
- A directed graph (or digraph) G :
- consists of a set V of vertices, and
- a set E of edges
- each edge e E is associated with an ∈ ordered pair of vertices.

## Vertex and Edge

- Vertex
- Drawn as a node or a dot.
- Vertex set of G is usually denoted by V(G), or V

## ● Edge

- Drawn as a line connecting two vertices
- Represent relation between vertices
- The edge set of G is usually denoted by E(G), or E.
- If edge e is associated with vertices p and q :

'Edge e is incident on p and q ' ' p and q are adjacent vertices.'

- Graph G with vertices V and edges E, G=(V,E)

## Examples

<!-- image -->

Undirected Graph

V = { 1, 2 ,3, 4, 5, 6}

E = { {1,2}, {1,5}, {2,5}, {2,3}, {3,4}, {5,4}, {6,4} }

<!-- image -->

Directed Graph

V = { 1, 2 ,3, 4, 5, 6}

<!-- formula-not-decoded -->

## Isolated Vertices

- An isolated vertex is a vertex that is with not incident with any edge.
- Example : '4' is an isolated vertex.

<!-- image -->

## Paths

- If, from vertex p , after travel along 1 or more edges, we eventually reach vertex q:
- 'there is a path from p to q '
- Example (refer page 8):
- On undirected graph, there is a path from any vertex to any vertex.
- On directed graph, there is a path from (1) to (4)
- On directed graph, there is no path from (4) to (1)

## Type of Graphs

## Simple Graphs

- Parallel/Multiple edges : edges that are both incident with the same vertex pair.
- Loop : An edge incident on a single vertex.
- Simple Graphs : graphs with no parallel edges, no loops

<!-- image -->

simplegraph

nonsimplegraph with multiple edges

<!-- image -->

nonsimple graph with loops

<!-- image -->

## Weighted Graphs

- Graphs that have a number assigned to each edge are called weighted graphs.
- The number represents the weight of the edge.

```
Example: the weight of : {a,b} = 5 {b,d} = 2 Length of path a to d
```

<!-- formula-not-decoded -->

<!-- image -->

## What is weight?

- Depends on what you want to model.
- Example: in an airline system, you get 3 different graphs if you assign different values to the graphs.

<!-- image -->

<!-- image -->

<!-- image -->

## Complete Graphs

- A complete graph on n vertices, denoted K n , is a simple graph with n vertices in which there is an edge between every pair of distinct vertices .
- Examples:

<!-- image -->

<!-- image -->

## Complete Graphs

<!-- image -->

There are more ways to draw these complete graphs, but they represent the same since the 'shape' of the graph doesn't carry information in graph theory.

## Exercise

## How many edges can be found in K5?

## Bipartite Graphs

- Bipartite graph : a graph with all vertices decomposed into two disjoint sets V 1 and V 2 , such that:

<!-- formula-not-decoded -->

- Each edge on a bipartite graph incident one vertex in V 1 and one vertex in V 2 .

## Bipartite graphs (examples)

<!-- image -->

<!-- image -->

<!-- image -->

<!-- image -->

<!-- image -->

## Bipartite graphs (exercise)

- Explain why the following graphs is not a bipartite graph:

<!-- image -->

## Bipartite graphs (exercise)

- Explain why the following graphs is not a bipartite graph:

<!-- image -->

Clue : check v 4 , v 5 , v 6 .

## Components and Subgraphs

## Connectivity

- A graph is connected if
- you can get from any vertex to any other by following a sequence of edges OR
- any two nodes are connected by a path.
- A directed graph is strongly connected if there is a directed path from any node to any other node.

## Components

- Every disconnected graph can be split up into a number of connected components.

<!-- image -->

A graph with 10 vertices and 10 edges

## Components

- Every disconnected graph can be split up into a number of connected components.

<!-- image -->

<!-- image -->

<!-- image -->

A graph with 10 vertices and 10 edges A graph with 3 components

## Subgraphs

- A subgraph of a graph G = (V , E) is a graph H = (W, F), where W V and F E. ⊆ ⊆
- A subset of vertices and edges of the original graph.
- Example: G' is a subgraph of G, with some edges and vertex removed.

Graph G

<!-- image -->

One of the subgraph of G

<!-- image -->

## Exercise

- Find all subgraphs of the graph G having at least one vertex.

<!-- image -->

## Paths and Cycles

## Paths vs Cycles

- A simple path from v to w is a path from v to w with no repeated vertices.
- A cycle is a path of nonzero length from v to v with no repeated edges.
- A simple cycle is a cycle from v to v in which, except for the beginning and ending vertices that are both equal to v , there are no repeated vertices.

## Example

<!-- image -->

| Path                                                    | SimplePath?      | Cycle?           | SimpleCycle?    |
|---------------------------------------------------------|------------------|------------------|-----------------|
| (6,5,2,4,3,2,1) (6,5,2,4) (2,6,5,2,4,3,2) (5,6,2,5) (7) | No Yes No No Yes | No No Yes Yes No | No No No Yes No |

## Degree

- Number of edges incident on a vertex.
- For simple graphs:

The degree of 5 is 3

<!-- image -->

## Degree

- Number of edges incident on a vertex.
- For digraph:
- In-degree: Number of edges entering
- Out-degree: Number of edges leaving
- Degree = indeg + outdeg

<!-- image -->

outdeg(1)=2 indeg(1)=0

outdeg(2)=2 indeg(2)=2

outdeg(3)=1 indeg(3)=4

## Degree

- If G is a digraph then

<!-- formula-not-decoded -->

- If G is a graph with m edges, then

<!-- formula-not-decoded -->

- Number of Odd degree Nodes is even.

<!-- formula-not-decoded -->

The first term is a sum of even numbers, so the result is even.

The second term is a sum of odd numbers, but the result is even, so the |V odd | is even as well.

## Euler Cycles

- Euler cycle - a cycle in a graph G that includes all of the edges and all of the vertices of G.
- A graph G has an Euler cycle if and only if all vertices of non-zero degree belong to a single connected component and every vertex has an even degree.
- If G has a Euler cycle and it has only a few edges, we can usually find the Euler cycle by inspection.

## Euler and Seven Bridges of Königsberg

<!-- image -->

<!-- image -->

## Euler Cycles

- Examples

<!-- image -->

<!-- image -->

## Additional info

- A graph has a path with no repeated edges from v to w (v ≠ w) containing all the edges and vertices if and only if it is connected and v and w are the only vertices having odd degree.
- Example:

<!-- image -->

## Hamiltonian Cycles

- A cycle in a graph G that contains each vertex in G exactly once, except for the starting and ending vertex that appears twice.
- An Euler cycle visits each edge once, whereas a Hamiltonian cycle visits each vertex once.
- no easily verified necessary and sufficient conditions are known for the existence of a Hamiltonian cycle in a graph

A graph and its Hamiltonian cycle

<!-- image -->

## Traveling salesman problem

- Classical computer science problem.
- 'Given a list of cities and the distances between each pair of cities, what is the shortest possible route that visits each city exactly once and returns to the origin city'
- Finding Hamiltonian cycles contribute solutions to this problem.

## Representation of Graphs

## Representation of Graphs

- Sometime, two graphs 'look' different but actually they are the same.
- We say these graphs are isomorphic.
- Example:
- Despite the drawings are different, isomorphic graphs may find similar representation.

<!-- image -->

<!-- image -->

## Representation with Matrix

- Incidence Matrix
- V x E
- The rows represent the vertices and the columns represent the edges. The entry for row v and column e is 1 if e is incident on v and 0 otherwise.

<!-- image -->

|   1,2 |   1,5 |   2.3 |   2,5 |   3,4 |   4,5 |   4.6 |
|-------|-------|-------|-------|-------|-------|-------|
|     1 |     1 |     0 |     0 |     0 |     0 |     0 |
|     1 |     0 |     1 |     1 |     0 |     0 |     0 |
|     0 |     0 |     1 |     0 |     1 |     0 |     0 |
|     0 |     0 |     0 |     0 |     1 |     1 |     1 |
|     0 |     1 |     0 |     1 |     0 |     1 |     0 |
|     0 |     0 |     0 |     0 |     0 |     0 |     1 |

## Representation with Matrix

- Adjacent Matrix
- V x V
- Adjacent→1   not adjacent →0
- Or Edge Weights

<!-- image -->

```
1 23 4 5 6 1 0 1 0 0 1 0 2 1 0 1 0 1 0 0 1 0 0 0 4 0 0 1 0 1 I 1 1 0 1 0 0 6 0 0 0 1 0 0
```

## Representation with List

- Adjacency List (node list)
- An array of |V| lists, one for each vertex in V.
- For each v ∈ V, ADJ [ v ] points to all its adjacent vertices.

<!-- image -->

Node e List 122 235 33 435 534

## Representation with List

- Edge List
- Pairs (ordered if directed) of vertices
- Optionally weight and other data
- Example 1:

<!-- image -->

Edge List 12 12 23 25 33 43 45 53 54

## Representation with List

- Edge List
- Pairs (ordered if directed) of vertices
- Optionally weight and other data
- Example 2:

<!-- image -->

## Topological Distance

- A shortest path is the minimum path connecting two nodes.
- The number of edges in the shortest path connecting p and q is the topological distance between these two nodes, d p,q

## Distance Matrix

- | V | x | V | matrix D = ( d ij ) such that d ij is the topological distance between i and j .

<!-- image -->

```
1 45 6 1 0 一 2 2 1 m 2 T 0 1 2 1 3 3 2 1 0 1 2 2 4 2 2 1 0 1 1 I 1 2 1 0 2 6 3 3 2 1 2 0
```

## Trees

## Tree

- A tree is a connected undirected graph with no simple circuits (cycles).
- cannot contain multiple edges or loops.
- Examples of tree:

<!-- image -->

## Where we find trees in Computer Science

<!-- image -->

File structures

<!-- image -->

Game analysis

<!-- image -->

## Exercise : Are these trees?

<!-- image -->

## Exercise : Are these trees?

<!-- image -->

## Rooted Trees

- A rooted tree is a tree in which one vertex has been designated as the root and every edge is directed away from the root.
- We usually draw a rooted tree with its root at the top of the graph.

<!-- image -->

## Rooted trees - terminology

<!-- image -->

## Rooted trees - terminology

<!-- image -->

internal vertices (vertex with child) : a, b, c, g, h, j

## Rooted trees - terminology

<!-- image -->

## Rooted trees - terminology

<!-- image -->

Ancestors of e : c, b, a

## Rooted trees - terminology

<!-- image -->

Ancestors of e : c, b, a

Descendants of g : h, i, j, k, l, m

## m-ary trees

- A rooted tree is called an m-ary tree if every internal vertex has no more than m children.
- The tree is called a full m-ary tree if every internal vertex has exactly m children.
- For example: Binary tree - every vertex has at most 2 children.

<!-- image -->

3 m-ary trees. What are the values of m for each case?

## Some properties of trees

- A tree with n vertices has n -1 edges.
- A full m -ary tree with i internal vertices contains n = mi + 1 vertices.
- There are at most m h leaves in an m -ary tree of height h .

(height: number of levels excluded root)

## Example: a full 3-ary tree

<!-- image -->

Each parent has exactly 3 children : full ternary tree

4 internal vertices, so total vertices = (3)(4)+1 = 13

13 vertices: 13-1 = 12 edges

2 levels → Not more than 3 2  = 9 leaves