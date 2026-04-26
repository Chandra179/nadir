## Problem Solving and Uninformed Search

limck@um.edu.my

|   1 | Problems Solving and Agents 1                                                           |
|-----|-----------------------------------------------------------------------------------------|
|   2 | Search Algorithms 3                                                                     |
|     | 2.1 Breadth First Search (BFS) . . . . . . . 5                                          |
|     | 2.1.1 Characteristics . . . . . . . . . . 5                                             |
|     | 2.1.2 Algorithm . . . . . . . . . . . . . 5                                             |
|     | 2.1.3 Performance of BFS . . . . . . . 7                                                |
|     | 2.2 Depth First Search (DFS) . . . . . . . . 9                                          |
|     | 2.3 Depth Limited Search (DLS) . . . . . . . 10                                         |
|     | 2.4 Iterative Deepening Search (IDS) . . . . 11                                         |
|     | 2.5 Dijkstra's Algorithm / Uniform-Cost Search . . . . . . . . . . . . . . . . . . . 13 |
|   3 | What is a 'problem'? 14                                                                 |
|   4 | Looking Forward 17                                                                      |

<!-- image -->

## 1 Problems Solving and Agents

A problem-solving agent is a type of rational agent that consider a sequence of actions that form a path to goal (state / vertex). To do so, search algorithms are required.

While this problem solving agent is not exactly the 'AI agent' that based on LLMs, the foundation is there. Planning and pathfinding is among the important technologies in robotics, reinforcement learning, and LLM-based agents.

A 4-step approach taken by a problem-solving agent in problem solving: (i) Goal formulation, (ii) Problem formulation, (iii) Search, (iv) Execution.

We define the goal clearly in the first, or the goal formation step. The goal is problem dependent. Note that some problems may have more than 1 possible goal.

| Example 1: Problems and       | Goals                                                             |
|-------------------------------|-------------------------------------------------------------------|
| Problems                      | Goals                                                             |
| I want to go to Penang        | To reach Penang                                                   |
| Play tic-tac-toe game         | To win Get 3 'X' in a row/column/diagonal in the tic-tac-toe game |
| Too tired, don't want to work | Get a donation of ✩ from Warren Buffett as retirement funding.    |

While goal formation defines the 'final destination' we want to reach, problem formulation define problem in precise terms. Firstly, we need to understand the initial state, what actions can we take, resulting states if actions are taken, etc. Eventually, we form an abstract model. For example, if we want to reach Penang, first, we need to confirm that we are currently in Kuala Lumpur (initial state). If we depart from Kuala Lumpur, we may reach Seremban, Klang, Tanjung Malim, etc.

Search is the main technique we are going to learn in this lecture. Agent simulate sequence of actions in the model, until it finds a sequence of actions that reach the goal.

Lastly, once problem-solving agent finds the goal, we execute the planned actions in the solution.

We can represent problems with graphs :

lim ck

- ❼ Nodes : States. Possible outcomes of some actions
- ❼ Edges : Actions. What an agent can do at a state.
- ❼ Weight : Cost function of actions. Numerical cost of applying an action.
- ❼ Graph : State space. A set of possible states.
- ❼ Path : A sequence of actions.

## 2 Search Algorithms

There are many search algorithms. Generally, a search algorithm takes a search problem as input and returns a solution. If a solution cannot be reached, it returns an indication of failure.

It keeps a few data structures or variables:

- ❼ Frontier list : a list of all possible next states/vertices that have been identified but not yet expended. (I know about these notes, but I haven't explored them)
- ❼ Reached set : also called explored set . States/nodes that have been visited/expended. (I have seen these nodes)
- ❼ node.STATE : the state to which the node corresponds;
- ❼ node.PARENT : the node in the tree that generated this node;
- ❼ node.ACTION : the action that was applied to the parent's state to generate this node
- ❼ node.PATH-COST : the total cost of the path from the initial state to this node. (we assume cost of each path as 1 for most of the algorithms we are going to learn in this chapter. It make sense for some
- of the problems, even though not all)
- ❼ IS-EMPTY (frontier) : returns true only if there are no nodes in the frontier.

lim ck

- ❼ POP(frontier) : removes the top node from the frontier and returns it.
- ❼ TOP(frontier) : returns (but does not remove) the top node of the frontier.
- ❼ ADD(node, frontier) : inserts node into its proper place in the frontier.

Refer to Figure 1 for a map of Romania. Assume that we are traveling there and we want to reach Bucharest from Arad.

Figure 1: Romania problem. We want to travel from Arad to Bucharest.

<!-- image -->

It seems easy to find the path from the map. However, actually what we really can 'see' is not a map like Figure 1, but just 'where we are', like Figure 2 if we do not explore to the neighbourhood.

Figure 2: What we can see if we are in Arad.

<!-- image -->

To solve our problem, we need a search strategy/algorithm. Each city is assume as a state in problem solving, represented

lim ck as a node/vertex in graph theory. Road linking the cities are actions that we can take, or edges. Our initial state is Arad, goal state is Bucharest.

Assume that we have an evaluation function f ( n ) that measure the depth of the node. It counts, for example, how many actions it takes to reach the goal.

In this chapter, we are going to learn a few uninformed search strategies, for example: breadth first search, depth first search and Dijkstra's algorithm.

We call them uninformed searches because we do not have information about how close a state is to the goal

## 2.1 Breadth First Search (BFS)

## 2.1.1 Characteristics

A few characteristic of BFS include:

- ❼ Root node expended first
- ❼ Successors are added to frontier and expended in sequence
- ❼ Successors are added with FIFO policy, i.e. it uses a queue for frontier
- ❼ Use a first-in-first-out queue
- ❼ Assume all actions have the same cost.
- ❼ Always find a solution with a minimal number of actions.

## 2.1.2 Algorithm

Algorithm 1 defines how BFS works.

In this algorithm (and other follow algorithms), it has an EXPEND function that does the following:

- 1) save the STATE of the node it wants to expend as s .
- 2) for each s ′ , the node it can explore from s :

lim ck

- a) Find cost from the following formula:

<!-- formula-not-decoded -->

(In BFS, the cost of each edge is 1, but this may not be the case for some algorithms)

- b) return a set of nodes ( s ′ ). For each one, record:
- i. state
- ii. parent node

iii. action that lead to

- iv. path cost

## Algorithm 1: Breadth-First Search

```
1 Function BREADTH-FIRST-SEARCH(problem) 2 node ← NODE( problem .INITIAL ) 3 if problem .IS-GOAL( node .STATE) then 4 return node 5 frontier ← a FIFO queue, with node as an element 6 reached ←{ problem.INITIAL } 7 while not Is-Empty( frontier ) do 8 node ← Pop( frontier ) 9 foreach child in Expand( problem , node ) do 10 s ← child.STATE 11 if problem .IS-GOAL(s) then 12 return child 13 if s is not in reached then 14 add s to reached 15 add child to frontier 16 return failure
```

## Example 2: Breadth-First

Using breadth first search, find the path from Arad to Bucharest. Assume the algorithm scan in clockwise direction, starting from the 12 o'clock position. Find a tree as the result of the search.

lim ck

<!-- image -->

## 2.1.3 Performance of BFS

BFS is a complete algorithm, which means that it finds a solution if there is one. Furthermore, it finds optimal solution - a solution with lowest path cost among all solutions.

Branching factor refers to the number of children generated from a node in the search tree. Let's examine BFS in term of branching factor.

Assume root node generates b children, the branching factor is b . After the first iteration, we find 1 + b nodes.

For the children of root, if each of them also have branching factor b , the children in layer 2 is b 2 .

If this go one, and we find the goal in layer d , total nodes we generated is:

<!-- formula-not-decoded -->

So, both the space and time complexity of BFS is O ( b d ).

If you think that BFS is useful and efficient, please consider to use it to find a path from UM (Kuala Lumpur) to Melaka. Is

lim ck what you imaging is something like Figure 3?

Figure 3: Search graph from KL to Melaka.

<!-- image -->

Figure 4: Part of UM and it's surrounding.

<!-- image -->

Zoom in to the map where UM is in Kuala Lumpur. You find a map in Figure 4. Do you find what was wrong?

According to the text book 'Russell, S., &amp; Norvig, P. Artificial intelligence: a modern approach; 2020; 4 London. UK Pearson.':

!

As a typical real-world example, consider a problem with branching factor b = 10, processing speed 1 million nodes/second, and memory requirements of 1 Kbyte/node. A search to depth d = 10 would take less than 3 hours, but would require 10 terabytes of memory ... At depth d = 14, even with infinite memory, the search would take 3.5 years.

lim ck

## 2.2 Depth First Search (DFS)

Compared to BFS, it uses a stack to store successors on the frontier (last in, first out policy). While it is doing the search, it returns the goal immediately when it is found, which makes DFS not a cost-optimal algorithm.

To avoid an infinite loop, DFS needs to check if a cycle exists in the graph.

If the state space is infinite, it may not be able to find a solution. In this case, DFS is incomplete.

If b is the branching factor and the maximum depth of the tree is m, the space complexity is O ( bm ) and the time complexity is O ( b m ).

## Algorithm 2: Depth-First Search

```
1 Function DEPTH-FIRST-SEARCH(problem) 2 node ← NODE( problem.INITIAL ) 3 frontier ← a LIFO stack, with node as an element 4 reached ←{ problem.INITIAL } 5 while not Is-Empty( frontier ) do 6 node ← POP( frontier ) 7 if problem . Is-Goal( node .STATE ) then 8 return node 9 foreach child in Expand( problem, node ) do 10 s ← child.STATE 11 if s is not in reached then 12 add s to reached 13 add child to frontier 14 return failure
```

## Example 3: Depth-First

Using DFS, find the path from Arad to Bucharest. Assume the algorithm scan in clockwise direction, starting from the 12 o'clock position. Find a tree as the result of the search.

lim ck

<!-- image -->

## 2.3 Depth Limited Search (DLS)

DLS is designed to keep DFS from wandering down an infinite path. In another words, it is a version of DFS with a depth limit l - assume no children after level l . However, if l is too small, we may not able to reach the goal.

One strategy of implementation of DLS is to set value of l based on knowledge of the problem. For example: If number of nodes is 20, l = 19. Additional info about the goal may improve the depth limit.

lim ck

## Algorithm 3: Depth-Limited Search

```
1 Function DEPTH-LIMITED-SEARCH(problem, ℓ ) 2 node ← NODE( problem.INITIAL ) 3 frontier ← a LIFO stack, with node as an element 4 result ← failure 5 while not Is-Empty( frontier ) do 6 node ← POP( frontier ) 7 if problem .IS-GOAL( node .STATE) then 8 return node 9 if Depth( node ) > ℓ then 10 result ← cutoff 11 else if not Is-Cycle( node ) then 12 foreach child in Expand( problem, node ) do 13 add child to frontier 14 return result
```

## Example 4: Depth Limited

From Arad to Bucharest, if you are using a DLS with l = , what would you find?

<!-- image -->

## 2.4 Iterative Deepening Search (IDS)

DLS is an improved version of DLS.

It works like DLS, but instead of choosing a good value for l , it tries all values: 0, 1, 2, . . . until it:

lim ck

- ❼ finds a solution, or
- ❼ returns failure
- ❼ returns cutoff when it reaches a limit

In another words, it defines value d = 1 , 2 , 3 , · · · , and it performs a DLS by l = d .

It is optimum like BFS.

But memory requirements are modest: O ( bd ) when there is a solution, or O ( bm ) on finite state spaces with no solution, where d is the depth of the solution, and m is the maximum depth of the tree.

## Algorithm 4: Iterative Deepening Search

̸

```
1 Function ITERATIVE-DEEPENING-SEARCH( problem ) 2 for depth ← 0 to ∞ do 3 result ← DEPTH-LIMITED-SEARCH( problem, depth ) 4 if result = cutoff then 5 return result
```

## Example 5: Iterative Deepening Search

IDS is used to search from Arad to Bucharest.

<!-- image -->

lim ck

## 2.5 Dijkstra's Algorithm / Uniform-Cost Search

BFS and DFS assume that the cost of each action/edge is 1, but this is only true for some problems. Dijkstra's algorithm considers the weight of each edge but requires them to be greater than 0.

If all action costs are equal, Dijkstra algorithm works exactly like BFS.

Dijkstra's algorithm is complete and cost optimal, but time and space complexity can be much grater than BFS.

## Algorithm 5: Dijkstra's Algorithm

```
1 Function UNIFORM-COST(problem) 2 node ← NODE( problem.INITIAL, path-cost=0 ) 3 frontier ← a Priority Queue ordered by PATH-COST, with node as an element 4 reached ← a lookup table, with one entry with key problem .INITIAL and value node 5 while not Is-Empty( frontier ) do 6 node ← POP( frontier ) 7 if problem .IS-GOAL( node .STATE) then 8 return node 9 foreach child in Expand( problem , node ) do 10 s ← child.STATE 11 if s is not in reached OR child .PATH-COST < reached [ s ].PATH-COST then 12 reached [ s ] ← child 13 add child to frontier 14 return failure ;
```

## Example 6: Dijkstra's Algorithm

Using Dijkstra's Algorithm, find the path from Arad to Bucharest. The cost of each edge is written in the figure. Assume the algorithm scan in clockwise direction. Find a tree as the result of the search.

lim ck Do you think that Waze or Google Map are using Dijkstra's algorithm?

<!-- image -->

## 3 What is a 'problem'?

Search algorithms are NOT only useful for finding routes on maps.

'State' does not only refer to a city on a map; it can represent any possible milestone in problem-solving.

'Action' does not only refer to traveling; it can represent any action that allows one to move to another possible milestone.

'Weight' does not only refer to distance; it can represent any cost for moving from one state to another.

A problem-solving agent can solve problems formulated as search problems, where the agent can clearly define the initial state, possible actions, and goal state. These problems usually have a known environment, predictable results of actions taken,

lim ck and a clear goal.

A few examples are given below:

## Example 7: Vacuum planning

A two-cell vacuum planning problem where robots have actions: R (move right), L (move left), S (suck to clean). Each condition of the cells is a state.

<!-- image -->

## Example 8: Sliding-tile puzzle

Each configuration of tiles in the puzzle is a state. Sliding a tile adjacent to a space into the space is a valid action.

StartState

<!-- image -->

GoalState

<!-- image -->

8-puzzle

What is the cost of an action?

lim ck

15-puzzle

<!-- image -->

## Example 9: Touring problem

It is also a variant of the path planning problem. For example, the traveling salesman problem with a limited cost. The initial state is the current city. Moving to an unvisited city is a valid action, and the goal is to come back to the initial city when all other cities are visited

vacuum planning

<!-- image -->

## Example 10: Assembly problem

This involves automatic assembly sequencing of complex objects (e.g., electrical vehicles). The aim is to find a good and optimal order in assembling the parts of the object.

<!-- image -->

lim ck

## 4 Looking Forward

All the search algorithms presented in this chapter are uninformed search, which search almost 'blindly' to all neighbours of current state.

They can be improved if we have more problem-specific knowledge (heuristic). For example: If you want to go to Penang from KL, you may explore Tanjung Malim or Sekinchan, but you may not consider Seremban. Informed search with heuristic (e.g. estimates distance to the goal).

<!-- image -->

lim ck