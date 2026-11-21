package analytics

import (
	"cmp"
	"context"
	"math"
	"net/url"
	"slices"
	"sync"
	"time"
)

type bskyPost struct {
	URI    string `json:"uri"`
	Author struct {
		DID string `json:"did"`
	} `json:"author"`
	Record struct {
		Text      string    `json:"text"`
		CreatedAt string    `json:"createdAt"`
		Reply     *struct{} `json:"reply"`
	} `json:"record"`
	Likes   *int `json:"likeCount"`
	Replies *int `json:"replyCount"`
	Reposts *int `json:"repostCount"`
	Quotes  *int `json:"quoteCount"`
}

type bskyProfile struct {
	Followers *int `json:"followers"`
	Following *int `json:"following"`
	Posts     *int `json:"posts"`
}

type bskyActivity struct {
	Posts       int     `json:"posts"`
	Replies     int     `json:"replies"`
	PostsPerDay float64 `json:"postsPerDay"`
}

type bskyCoverage struct {
	FeedComplete      bool `json:"feedComplete"`
	FeedAvailable     bool `json:"feedAvailable"`
	SharesAvailable   bool `json:"sharesAvailable"`
	SharesComplete    bool `json:"sharesComplete"`
	ArticlesSearched  int  `json:"articlesSearched"`
	FeedPostsExamined int  `json:"feedPostsExamined"`
}

type bskyDisplayPost struct {
	URI         string      `json:"uri"`
	Title       string      `json:"title"`
	URL         string      `json:"url"`
	PublishedAt string      `json:"publishedAt"`
	Engagement  *Engagement `json:"engagement"`
}

type bskyShare struct {
	ArticleURI   string            `json:"articleUri"`
	ArticleTitle string            `json:"articleTitle"`
	ArticleURL   string            `json:"articleUrl"`
	Posts        []bskyDisplayPost `json:"posts"`
}

type bskySummary struct {
	SampledAt string            `json:"sampledAt"`
	Days      int               `json:"days"`
	Profile   bskyProfile       `json:"profile"`
	Activity  *bskyActivity     `json:"activity"`
	TopPosts  []bskyDisplayPost `json:"topPosts"`
	Shares    []bskyShare       `json:"shares"`
	Coverage  bskyCoverage      `json:"coverage"`
}

func (p Provider) bluesky(ctx context.Context, author string, days int, articles []Item, at time.Time) bskySummary {
	since := at.AddDate(0, 0, -days)
	var group sync.WaitGroup
	var profile bskyProfile
	var feed bskyFeedResults
	var shares bskyShareResults
	group.Go(func() { profile = p.blueskyProfile(ctx, author) })
	group.Go(func() { feed = p.blueskyFeed(ctx, author, since, at) })
	group.Go(func() { shares = p.blueskyShares(ctx, articles, since, at) })
	group.Wait()
	summary := bskySummary{SampledAt: timestamp(at), Days: days, Profile: profile,
		TopPosts: make([]bskyDisplayPost, 0), Shares: shares.shares,
		Coverage: bskyCoverage{FeedComplete: feed.complete, FeedAvailable: feed.available,
			SharesAvailable: shares.available, ArticlesSearched: shares.searched, FeedPostsExamined: feed.examined}}
	if feed.available {
		summary.Activity = &bskyActivity{}
		for _, post := range feed.posts {
			if post.Record.Reply == nil {
				summary.Activity.Posts++
			} else {
				summary.Activity.Replies++
			}
			summary.TopPosts = append(summary.TopPosts, presentPost(post))
		}
		summary.Activity.PostsPerDay = float64(summary.Activity.Posts) / float64(days)
		slices.SortFunc(summary.TopPosts, func(a, b bskyDisplayPost) int {
			if byScore := cmp.Compare(score(b.Engagement), score(a.Engagement)); byScore != 0 {
				return byScore
			}
			if byTime := cmp.Compare(b.PublishedAt, a.PublishedAt); byTime != 0 {
				return byTime
			}
			return cmp.Compare(a.URI, b.URI)
		})
		summary.TopPosts = summary.TopPosts[:min(10, len(summary.TopPosts))]
	}
	return summary
}

func (p Provider) blueskyProfile(ctx context.Context, author string) bskyProfile {
	var response struct {
		DID       string `json:"did"`
		Followers *int   `json:"followersCount"`
		Following *int   `json:"followsCount"`
		Posts     *int   `json:"postsCount"`
	}
	err := p.publicJSON(ctx, "https://public.api.bsky.app/xrpc/app.bsky.actor.getProfile",
		url.Values{"actor": {author}}, 3*time.Second, 2<<20, &response)
	if err != nil || response.DID != author {
		return bskyProfile{}
	}
	count := func(value *int) *int {
		if value == nil || *value < 0 {
			return nil
		}
		return value
	}
	return bskyProfile{count(response.Followers), count(response.Following), count(response.Posts)}
}

type bskyFeedResults struct {
	posts     []bskyPost
	complete  bool
	available bool
	examined  int
}

func (p Provider) blueskyFeed(ctx context.Context, author string, since, until time.Time) bskyFeedResults {
	var result bskyFeedResults
	seen := map[string]bool{}
	cursors := map[string]bool{}
	query := url.Values{"actor": {author}, "limit": {"100"}, "filter": {"posts_with_replies"}, "includePins": {"false"}}
	for range 3 {
		var response struct {
			Feed []struct {
				Post   bskyPost  `json:"post"`
				Reason *struct{} `json:"reason"`
			} `json:"feed"`
			Cursor string `json:"cursor"`
		}
		err := p.publicJSON(ctx, "https://public.api.bsky.app/xrpc/app.bsky.feed.getAuthorFeed",
			query, 3*time.Second, 2<<20, &response)
		if err != nil || response.Feed == nil || len(response.Feed) > 100 {
			result.available = len(result.posts) > 0
			return result
		}
		result.examined += len(response.Feed)
		for _, row := range response.Feed {
			if row.Reason == nil && row.Post.Author.DID == author && validPost(row.Post, seen, since, until) {
				result.posts = append(result.posts, row.Post)
			}
		}
		result.available = true
		if response.Cursor == "" {
			result.complete = true
			break
		}
		if cursors[response.Cursor] {
			break
		}
		cursors[response.Cursor] = true
		query.Set("cursor", response.Cursor)
	}
	return result
}

type bskyShareResults struct {
	shares    []bskyShare
	available bool
	searched  int
}

func (p Provider) blueskyShares(ctx context.Context, articles []Item, since, until time.Time) bskyShareResults {
	eligible := make([]Item, 0, 5)
	urls := map[string]bool{}
	articles = slices.Clone(articles)
	slices.SortStableFunc(articles, func(a, b Item) int { return cmp.Compare(b.PublishedAt, a.PublishedAt) })
	for _, article := range articles {
		target, err := url.Parse(article.URL)
		if article.Kind != "article" || err != nil || target.Scheme != "https" || target.Hostname() == "" ||
			target.User != nil || urls[article.URL] {
			continue
		}
		eligible = append(eligible, article)
		urls[article.URL] = true
		if len(eligible) == 5 {
			break
		}
	}
	out := make(chan *bskyShare, len(eligible))
	for _, article := range eligible {
		go func() {
			var response struct {
				Posts []bskyPost `json:"posts"`
			}
			query := url.Values{"q": {"*"}, "url": {article.URL}, "sort": {"latest"},
				"since": {since.UTC().Format(time.RFC3339)}, "until": {until.UTC().Format(time.RFC3339)}, "limit": {"25"}}
			err := p.publicJSON(ctx, "https://api.bsky.app/xrpc/app.bsky.feed.searchPosts",
				query, 3*time.Second, 2<<20, &response)
			if err != nil || response.Posts == nil || len(response.Posts) > 25 {
				out <- nil
				return
			}
			item := &bskyShare{ArticleURI: article.URI, ArticleTitle: article.Title, ArticleURL: article.URL, Posts: make([]bskyDisplayPost, 0)}
			seen := map[string]bool{}
			for _, post := range response.Posts {
				if validPost(post, seen, since, until) {
					item.Posts = append(item.Posts, presentPost(post))
				}
			}
			out <- item
		}()
	}
	result := bskyShareResults{shares: make([]bskyShare, 0, len(eligible)), available: true, searched: len(eligible)}
	for range eligible {
		if item := <-out; item != nil {
			result.shares = append(result.shares, *item)
		} else {
			result.available = false
		}
	}
	slices.SortFunc(result.shares, func(a, b bskyShare) int { return cmp.Compare(a.ArticleURI, b.ArticleURI) })
	return result
}

func validPost(post bskyPost, seen map[string]bool, since, until time.Time) bool {
	owner, collection, _, err := Subject(post.URI)
	if err != nil || owner != post.Author.DID || collection != postCollection || seen[post.URI] {
		return false
	}
	seen[post.URI] = true
	published, err := time.Parse(time.RFC3339Nano, post.Record.CreatedAt)
	return err == nil && !published.Before(since) && !published.After(until)
}

func presentPost(post bskyPost) bskyDisplayPost {
	owner, _, rkey, _ := Subject(post.URI)
	view := bskyDisplayPost{URI: post.URI, Title: post.Record.Text, URL: "https://bsky.app/profile/" + owner + "/post/" + rkey,
		PublishedAt: post.Record.CreatedAt}
	if post.Likes != nil && post.Replies != nil && post.Reposts != nil && post.Quotes != nil &&
		min(*post.Likes, *post.Replies, *post.Reposts, *post.Quotes) >= 0 {
		view.Engagement = &Engagement{*post.Likes, *post.Replies, *post.Reposts, *post.Quotes}
	}
	return view
}

func score(engagement *Engagement) int {
	if engagement == nil {
		return -1
	}
	total := 0
	for _, value := range []int{engagement.Likes, engagement.Replies, engagement.Reposts, engagement.Quotes} {
		total += min(math.MaxInt-total, value)
	}
	return total
}
