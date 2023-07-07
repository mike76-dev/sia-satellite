package hosttree

type byScore []hostEntry

func (he byScore) Len() int           { return len(he) }
func (he byScore) Less(i, j int) bool { return he[i].score.Cmp(he[j].score) < 0 }
func (he byScore) Swap(i, j int)      { he[i], he[j] = he[j], he[i] }
