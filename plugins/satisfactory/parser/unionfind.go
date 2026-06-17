package main

// unionFind is a disjoint-set over comparable keys with iterative find +
// path compression. Iterative (not recursive) so that an adversarial save
// producing a long belt/pipe chain cannot overflow the goroutine stack — the
// parser must always emit a clean error, never crash (see main.go).
type unionFind[K comparable] struct {
	parent map[K]K
}

func newUnionFind[K comparable]() *unionFind[K] {
	return &unionFind[K]{parent: map[K]K{}}
}

// find returns the representative of x's set, registering x as its own set on
// first sight. Path compression flattens the traversed chain to the root.
func (u *unionFind[K]) find(x K) K {
	if _, ok := u.parent[x]; !ok {
		u.parent[x] = x
		return x
	}
	root := x
	for u.parent[root] != root {
		root = u.parent[root]
	}
	for x != root {
		next := u.parent[x]
		u.parent[x] = root
		x = next
	}
	return root
}

func (u *unionFind[K]) union(a, b K) {
	u.parent[u.find(a)] = u.find(b)
}
