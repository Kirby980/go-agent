package tool

type weatherArgs struct {
	City string `json:"city" desc:"城市名，如 北京"`
	Days int    `json:"days,omitempty" desc:"预报天数，默认 1"`
}
