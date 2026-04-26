## Games and Search

limck@um.edu.my

## Contents

| 1                   | Games 2                                                                                                        |
|---------------------|----------------------------------------------------------------------------------------------------------------|
| 2                   | MINIMAX Algorithm and Adversarial Two-player Zero-sum Games 5                                                  |
| 3                   | Alpha-beta Pruning 8 3.1 Why pruning . . . . . . . . . . . . . . . 8                                           |
| 3.2 4 Heuristic 4.1 | Alpha-beta Search Algorithm . . . . . . 8 Alpha-Beta Tree Search 11 Evaluation functions . . . . . . . . . . . |
| 4.2                 | 11 Cutting off search . . . . . . . . . . . . . 12 13                                                          |
| 5 Monte 5.1         | Carlo Tree Search (MCTS) 14 Background . . . . . . . . . . . . . . . . 14                                      |
| 4.3                 | Forward pruning . . . . . . . . . . . . . 14                                                                   |
| 4.4                 | Search versus lookup . . . . . . . . . . .                                                                     |
| 5.2                 | Algorithm . . . . . . . . . . . . . . . . . 14                                                                 |
| 5.3                 | Variants . . . . . . . . . . . . . . . . . . 17                                                                |

Let's start with a game! Visit the link given.

## 1 Games

Remember the 8-puzzle game? It is only one of the games we played. Besides 8-puzzle, there are some other games with different characteristics, including:

- ❼ one player, two players, vs multiplayer games.
- ❼ deterministic vs stochastic.
- ❼ perfect information vs imperfect information.

Not all games are zero-sum game . A zero-sum game is a two player or multiplayer game in which one player's gains come exactly at the expense of another players' losses.

Among all, one common measurement of complexity of games is state-space complexity. While space complexity measures the amount of memory an algorithm needs to run to completion, state-space complexity is the number of legal game positions obtainable from the initial position of the game. Usually, statespace complexity is expressed as a power of 10. For example, a state-space complexity of 10 3 means that a game has approximately 1,000 unique legal positions possible throughout the entire game, including unfinished states and (usually) excluding symmetries.

Games are a primary focus of AI research. Beyond their commercial appeal, they serve as 'mini-universes' with fixed rules, environments, and objectives, providing a controlled setting to evaluate the effectiveness of AI systems. With the experience in games, the logic used to win could be translates to solving critical real life issues.

Game theory is the study of mathematical models of strategic interactions - including games, and other domains like economics, social sciences and computer sciences. In general, it study how people and organizations make decisions in situations where their choices affect one another. At first, game theory focused on twoplayer zero-sum games. In the 1950s, it expanded to include non-zero-sum games, where everyone's outcomes can improve or

lim ck zero-sum game state-space complexity games as mini-universes game theory worsen. Now, game theory is a broad term that refers to understanding rational decision-making in humans, animals, and computers. Important figures in the development of game theory include mathematicians John von Neumann and John Nash.

Example 1 and 2 show a few popular single - and two/multiplayer games .

## Example 1: Single Player Games

Some of the well known Single player games include:

Figure 1: Examples of single player games

<!-- image -->

Rubrik's cube is a deterministic, perfect information game, compared to minesweeper, which has imperfect information. Do you agree that RPG games are stochastic and imperfect information?

lim ck single player games two/multi-player games

## Example 2: Two or Multi Player Games

Some of the well known two or multi player games include:

<!-- image -->

- (e) Snakes &amp; Ladders (n,s,p)

(f) Mahjong (4,s,ip)

Figure 2: Examples of two or multiplayer player games

All the games mentioned above are turn-taking , zero-sum games, but they are not identical. In addition to the number of players, factors such as whether the games are deterministic or stochastic, and whether they involve perfect or imperfect information, as well as variations in settings and rules, contribute to the complexity of each game. The state-space complexity is 10 3 for tic-tac-toe, 10 44 for chess, and 10 170 for Go. In term of average branching factor, 4 for tic-tac-toe, 35 for chess, and 250 for Go.

lim ck turn-taking

## 2 MINIMAX Algorithm and Adversarial Two-player Zero-sum Games

The objective for a player in a game is to win. To model this objective, a utility function is used. Autility function (also called an objective function or payoff function) defines the final numeric value to player when a game is ended. For example, win in a chess has value 1, draw has value 0 and loss has value -1. For Go game, we can use the same definition like chess, however measure the territory conquered is another way to measure value of the game.

A few definition in a 2-player zero-sum game include:

- ❼ s : a state
- state /position in a game
- ❼ s 0 : The initial state initial state , also the root in a search tree
- ❼ Actions ( s ) : a set of legal moves in state
- actions s
- ❼ Result ( s , a ) : returns another state, i.e. the state resulting from taking action a at state s .
- ❼ Is-Terminal ( s ) : a test to find out if s is the end of a game. Returns TRUE or FALSE.
- ❼ Utility ( s, p ) : defines the final numeric value to player p when the game ends in terminal state s .

In a zero-sum game, a player always wants to maximize their utility/value, while the adversary always wants to minimize that player's value (equivalent to increase the adversary's value). Let's call this player MAX and the adversary MIN, and we can study the playing of tic-tac-toe using a game tree (Figure 3). In this case, MAX plays 'X' and MIN plays 'O'.

In the search tree, first layer is move by MAX, next layer by MIN, and alternate to terminal states. Each layer in the game tree is called a ply .

Deciding on a move is a decision-making challenge that can be viewed as a search problem . We must explore sequences of moves that result in a winning outcome. MAX always want to

lim ck result Is-Terminal Utility MAX and MIN game tree ply search problem utility function, objective function, payoff function value find a strategy to reach highest value, but MIN is against MAX. Let's examine this with a simpler game tree in Example 3).

Figure 3: A partial game tree for the game of tic-tac-toe.

<!-- image -->

- ❼ B : MIN will choose

b 1 to return 3.

- ❼ C : MIN will choose

c 1 to return 2.

- ❼ D : MIN will choose d

3 to return 2.

<!-- image -->

lim ck In this case, to achieve highest value, the optimal move for MAX is a 1 in order to obtain value 3 at the end, if MIN is also playing optimally.

The steps taken are actually following the MINIMAX algorithm (Algorithm 1). A fully built game tree is the input of the algorithm. With a complete depth-first search, the algorithm explore the game tree to find the best move for MAX. In short, the algorithm works as follow recursively: At each MAX node, pick the move with maximum value; at each MIN node, pick the move with minimum value; at the root, determine the optimal move.

## Algorithm 1: MINIMAX

```
1 Function MINIMAX-SEARCH(game, state) 2 player ← game. to-move ( state ) 3 value, move ← max-value ( game,state ) 4 return move 5 Function MAX-VALUE(game, state) 6 if game. is-terminal ( state ) then 7 return game. utility ( state, player ) , null 8 v ←-∞ 9 foreach a in game. actions ( state ) do 10 v 2 , a 2 ← min-value ( game,game. result ( state, a )) 11 if v 2 > v then 12 v, move ← v 2 , a 13 return v, move 14 Function MIN-VALUE(game, state) 15 if game. is-terminal ( state ) then 16 return game. utility ( state, player ) , null 17 v ← + ∞ 18 foreach a in game. actions ( state ) do 19 v 2 , a 2 ← max-value ( game,game. result ( state, a )) 20 if v 2 < v then 21 v, move ← v 2 , a 22 return v, move
```

lim ck MINIMAX

## 3 Alpha-beta Pruning

## 3.1 Why pruning

Exercise: draw a complete game tree for tic tac toe.

Developing a complete game tree is costly. Image how many branches we can have for games like chess. It is a challenge to computers - if the maximum depth of the tree is m and there are b legal moves at each point, then the time complexity of the MINIMAX algorithm is O ( b m ).

Vanilla MINIMAX is impractical for complex games.

But, we can use a technique to cut some of the branches of the game tree, so that we do not need to deal with the complete game tree. We call it alpha-beta pruning .

Consider the game tree in Example 3 again. It is obvious that no matter what is the value brings by actions c 2 and c 3 , as long as they are not smaller than 2, the outcome of game is the same - they can be pruned.

What if there are children after these two nodes, i.e. there are roots of subtrees? Same!

## 3.2 Alpha-beta Search Algorithm

Alpha-beta search gets its name from the two extra parameters in MAX-VALUE (game, state, α , β ) of the algorithm (Algo. 2):

- ❼ α = the best (highest) value found so far for MAX along any path
- ❼ β = the best (lowest) value found along any path for MIN.

A branch can be pruned if α ≥ β because the branch has no affect to final result.

lim ck pruning Alpha-beta search

## Algorithm 2: Alpha-Beta Search

```
1 Function ALPHA-BETA-SEARCH(game, state) 2 player ← game. to-move ( state ) 3 value, move ← max-value ( game,state, -∞ , + ∞ ) 4 return move 5 Function MAX-VALUE(game, state, α , β ) 6 if game. is-terminal ( state ) then 7 return game. utility ( state, player ) , null 8 v ←-∞ 9 foreach a in game. actions ( state ) do 10 v 2 , a 2 ← min-value ( game,game. result ( state, a ) , α, β ) 11 if v 2 > v then 12 v, move ← v 2 , a 13 α ← max ( α, v ) 14 if v ≥ β then 15 return v, move // Pruning step 16 return v, move 17 Function MIN-VALUE(game, state, α , β ) 18 if game. is-terminal ( state ) then 19 return game. utility ( state, player ) , null 20 v ← + ∞ 21 foreach a in game. actions ( state ) do 22 v 2 , a 2 ← max-value ( game,game. result ( state, a ) , α, β ) 23 if v 2 < v then 24 v, move ← v 2 , a 25 β ← min ( β, v ) 26 if v ≤ α then 27 return v, move // Pruning step 28 return v, move
```

How the algorithms work:

1. It performs a depth first search starts from the root node, and brings along the value of α and β wherever it visits. The initial values of α = -∞ and β = + ∞ .

lim ck

2. At any MAX node, MAX only cares about the value of α . With max(), it compares the value of α it holds with any values from the children, including α and β . Take note that this comparison follows sequence.
3. At any MIN node, MIN only cares about the value of β . With min(), it compares the value of β it holds with any values from the children, including α and β . Take note that this comparison follows sequence.
4. In any comparison in the steps above, as long as it finds α ≥ β , all the other branches are pruned.
5. The values can be return together as like MINIMAX.

## Example 4: Alpha-Beta Search

With Alpha-beta search algorithm, prune branches and find the value of the root node for the following game tree.

<!-- image -->

Refer to this website for more examples with animation: AlphaBeta Pruning Practice

The effectiveness of alpha-beta pruning is highly dependent on the order in which states are expanded. For the purposes of this course, we will follow the convention of examining nodes from left to right. Note, however, that 'left' and 'right' are conceptual simplifications used for visualization; in actual tree data structures, the order is determined by how successor states are generated or indexed

lim ck

## 4 Heuristic Alpha-Beta Tree Search

Even with alpha-beta pruning, space and time are still issues on larger games like chess and Go. Some studies find that to keep all the state space of Go in a computer, we need 10 171 bytes. As a comparison, we only have about 10 78 -10 82 atoms in the universe.

Again, heuristic functions could be an answer:

- i Apply a heuristic evaluation function (refer 4.1) to states. With this, we can even treat nonterminal nodes as if they were terminal.
- ii Replace the terminal test by a cutoff test (refer 4.2). To be compatible to classical alpha-beta search, this cutoff test must return TRUE for terminal nodes.

## 4.1 Evaluation functions

Define a heuristic evaluation function Eval ( s, p ) that estimates the expected value of state s to player p . Among the requirements of this heuristic evaluation function:

- i. For a terminal state s , fulfill:

<!-- formula-not-decoded -->

- ii. For a nonterminal state s , fulfill:

<!-- formula-not-decoded -->

Other than that, a good heuristic also:

- i. Efficiency Efficiency : Computation time must be minimal to enable faster searching .
- ii. Relevance : The evaluation function should have a strong correlation with the actual chances of winning.

lim ck Relevance terminal test, cutoff test heuristic evaluation function There are many ways to define a evaluation functions. One of these methods is calculating the features of the state. For example, 'material value ' introduced in chess books can be refereed to define the heuristic values.

## Example 5: Evaluation function of chess

Some chess textbooks suggest that each piece carries a value: each pawn is worth 1, a knight or bishop is worth 3, a rook 5, and the queen 9. Other features such as 'good pawn structure' and 'king safety' might be worth half a pawn. We can use a weighted linear function for evaluation:

<!-- formula-not-decoded -->

where each f i is a feature of the position (such as 'number of white bishops') and each w i is a weight.

Note that the increment of evaluation value indicate the higher chance to win. However, evaluation value doesn't need to growth linearly with the chance of winning.

## 4.2 Cutting off search

The Alpha-Beta Search algorithm (Algo 2) needs to be improved with this newly introduced Eval ( s, p ).

Furthermore, not only we want to check if a node is terminal, but if the node can be cutoff .

Two sections (line 6-7 and 18-19) of the code in Algo 2 need to be replaced with the following:

<!-- formula-not-decoded -->

Note that 'depth' is a parameter in the cutoff function.

Most of the time, we cannot afford to complete searches of each branches until the terminal leaves. Therefore, we need to cutoff a search of a child node and assume it as a terminal.

The easiest way - fix a constant, depth in the algorithm. How-

lim ck material value cutoff ever, good depth not only depends on the time given to a move in a game, but also depends on the speed of computers and other considerations.

A better way is to use the iterative deepening technique in search:

- i) set depth = 1, perform searches, and store the best result. If time allows, proceed to the next step.
2. ii) set depth = 2, perform searches, and store the best result. If time allows, proceed to the next step.
3. iii) set depth = 3, perform searches, and store the best result. If time allows, proceed to the next step.
4. iv) set depth = 4, . . .

When the time runs out, the program returns the move selected by the deepest completed search.

Computers make mistakes because of this. Imaging that a bad move at depth = 4, (for example, let the queen being captured by opponent), but at depth = 5, it leads to a good move (checkmate with a bishop).

## 4.3 Forward pruning

Forward pruning prunes moves that appear to be poor moves. But, because reasons, including incomplete evaluation, limited depth and etc, some good moves might possible be pruned as well.

We have to bear the 'risk' when time and space are constrained.

Different strategies for pruning, including:

- i) Beam search : on each ply, consider only the best k moves reported by the evaluation function.
2. ii) Probcut algorithm : like alpha-beta search, but make use of statistics gained from previous experience to determine a window (like alpha and beta) to prune.

lim ck Beam search Probcut algorithm

## 4.4 Search versus lookup

Instead of search, many game-playing programs use 'table lookup ' for some portion of the game, especially at the beginning and ending of games.

Actually, the idea like table lookup is not only used in AI programs, but by human, for example Opening Theory in chess and Joseki ( ) in Go.

## 5 Monte Carlo Tree Search (MCTS)

## 5.1 Background

Monte Carlo Tree Search gained widespread public attention after AlphaGo defeated world Go champions. Actually, it was already well known in AI and game research long before AlphaGo.

MCTS does not use heuristic evaluation function.

Instead, it performs a simulation , or playout, for one player, than for the other, repeat until a terminal position is reached. The result (win/lost/value) at the terminal is recorded for this playout. With many playouts on the same node, it determine a move is good or not from the statistics of the results.

## 5.2 Algorithm

## Algorithm 3: Monte-Carlo Tree Search

```
1 Function MONTE-CARLO-TREE SEARCH(state) 2 tree ← node ( state ) 3 while is-time-remaining () do 4 leaf ← select ( tree ) 5 child ← expand ( leaf ) 6 result ← simulate ( child ) 7 back-propagate (result, child ) 8 return the move in actions (state) whose node has highest number of playouts
```

lim ck table lookup Monte Carlo Tree Search AlphaGo simulation or playout Fundamentally, MCTS is all about running through four steps: selection, expansion, simulation, and back-propagation and over again (Refer Algorithm 3). Ideally, it keeps going forever, but in reality, it is limited by time and resources.

- 1) Selection : Starting at the root, we recursively apply the selection policy (UCB, discuss later) to navigate down the tree. At each level, the policy chooses the best move for the player whose turn it is. This continues until we reach a node that is not fully expanded (it has at least one child move not yet in the tree) or is a terminal state.
- 2) Expansion : Generating one or more new child nodes to grow the tree. Each node represents a possible action at the state.
- 3) Simulation : Perform a playout from the newly generated child node. Simulated moves are taken by the player and the opponents in sequence. The moves can be chosen randomly or follow some simple heuristic that does not require heavy computational cost. Note that these moves are not recorded in the search tree.
- 4) Back-propagation : We use the playout result to update all the relevant search tree nodes. Statistics to update include visit counts and win rate.

Upper Confidence Bound (UCB1) is one of the most popular algorithms used to form the selection policy. It finds balance between exploitation (going for moves that generally give a better average reward) and exploration (trying out moves where we do not have a lot of info).

For a node n , the formula is:

<!-- formula-not-decoded -->

where:

U ( n ): total of values that went through node

n

N ( n ): number of visits through node

n

lim ck Selection Expansion Simulation Back-propagation Upper Confidence Bound exploitation exploration Parent ( n ): parent node of n in the tree The first term in the Eq (1) computes the average value of the player if the node is chosen (exploitation), while the term in square root gives priority to the nodes that have the opportunity to further explore (exploration). C is a constant that balances exploitation and exploration, with a theoretical value √ 2, but allows changes in practical.

## Example 6: Monte-carlo Tree Search

Consider an adversarial two-player game (black and white) with a search tree as follow. Given that all the internal nodes have been fully explored according to game rule. The root node, which labeled with 5:15 represents that there are 15 visits to this node, and 5 is the value of taking this move.

<!-- image -->

## Selection:

Computations of UCB1 find that the node labeled '10:15',

lim ck followed by '7:10' will be selected.

Expansion: Once we reached this leave node, we grow the tree by creating a new node and label it with '0:0'.

Simulation: In the simulation step, we perform a playout from this new node. This step is ended when it reaches a terminal node. At the terminal, it finds a win or lost result for the player.

Back propagation: Assume that the result for first simulation is a win for the white player, we update the results in back propagation step for the corresponding nodes.

Example 6 shows an application of MCTS in a 2-player adversarial game. MCTS finds many other applications, such as single player strategic games, maze solving, etc. In these applications, the selection and back propagation process is modified to focus on maximizing a single-player reward or finding a path to a goal, rather than alternating between minimizing and maximizing (minimax) for two opposing players.

## 5.3 Variants

An alternative way of computing the values is weighted sum . In this case, the values of parent node is computed with:

<!-- formula-not-decoded -->

lim ck weighted sum In Example 6, we assume that the root node is expended immediately when it is created. Therefore the total value and visit count of a parent node are equal to the sum of its children's respective values and visit counts. Some implementation of MCTS assume root node is firstly created and simulated before creating any child. In this case:

<!-- formula-not-decoded -->

lim ck immediate expension