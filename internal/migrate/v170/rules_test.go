package v170

import "testing"

// 规则单测锁定「尽力填充」语义：单一无歧义命中才写、多组/零命中不写、第三方指称不误判。

func TestMatchRelationKind(t *testing.T) {
	cases := []struct {
		name, describe, description, want string
	}{
		{"师父单命中", "他教他桩法，师父与徒弟", "", "mentor"},
		{"师傅不误判为kin", "师傅带他下水", "", "mentor"},
		{"闹翻即rival", "旧雇主与手艺人，已闹翻：扣工钱", "", "rival"},
		{"对峙同组多命中仍rival", "闹翻后的正面对峙：他要拿证据", "", "rival"},
		{"夫妻即lover", "夫妻：她带着孩子守在坑沿", "", "lover"},
		{"表侄即kin", "小满是他的表侄，头一回下深水", "", "kin"},
		{"之父即kin", "陈三斗之父，二十年不相往来", "", "kin"},
		{"多组命中不写", "从师徒到仇人，一场翻脸", "", ""},
		{"零命中不写", "素不相识，他三更破规矩把人带回来", "", ""},
		{"描述列也参与", "", "从赌约到托付：结拜之后欠一条人命", "kin"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := matchRelationKind(c.describe, c.description); got != c.want {
				t.Fatalf("matchRelationKind(%q,%q) = %q, want %q", c.describe, c.description, got, c.want)
			}
		})
	}
}

func TestMatchLocationState(t *testing.T) {
	cases := []struct {
		name string
		text []string
		want string
	}{
		{"被铁栅封住即blocked", []string{"通道口被一道铁栅封住。铁栅是新焊的"}, "blocked"},
		{"封着即blocked", []string{"再往里，通道被一道新焊的铁栅封着"}, "blocked"},
		{"坍塌即destroyed", []string{"庙门坍塌，仅存山墙"}, "destroyed"},
		{"塌出来不误判", []string{"前年塌出来的一口天坑，宽二十来米"}, ""},
		{"顶上塌了缺口不误判", []string{"顶上塌了一个缺口，一线天光漏下来"}, ""},
		{"多组命中不写", []string{"旧殿坍塌，入口被石板封死"}, ""},
		{"零命中不写", []string{"井口覆铁盖，盖上有一个三棱孔"}, ""},
		{"多文本合并", []string{"", "", `["封死"]`}, "blocked"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := matchLocationState(c.text...); got != c.want {
				t.Fatalf("matchLocationState(%v) = %q, want %q", c.text, got, c.want)
			}
		})
	}
}

func TestMatchCharacterGender(t *testing.T) {
	cases := []struct {
		name, charName, desc, want string
	}{
		{"之父即男", "陈守拙", "陈三斗之父，六十岁上下，名义上死在三峡", "男"},
		{"之母即女", "某氏", "陈三斗之母，早年病逝", "女"},
		{"姓名女人即女", "姓何的女人", "三十来岁，失踪潜水员何建国的家属", "女"},
		{"姓名夹克男即男", "姓李的夹克男", "上面来的三个人里出面说话的那个", "男"},
		{"第三方他娘不误判", "小满", "二十出头，周德贵的表侄，他娘让周德贵带他出来", ""},
		{"他的妻子不误判", "何建国", "失踪的潜水员，他的妻子守在坑沿", ""},
		{"姓名与描述冲突不写", "某男", "陈三斗之母", ""},
		{"零命中不写", "陈三斗", "28岁，退役水下工兵", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := matchCharacterGender(c.charName, c.desc); got != c.want {
				t.Fatalf("matchCharacterGender(%q,%q) = %q, want %q", c.charName, c.desc, got, c.want)
			}
		})
	}
}

func TestMatchCharacterBirthYear(t *testing.T) {
	cases := []struct {
		name, desc, want string
	}{
		{"阿拉伯数字年龄", "28岁，退役水下工兵，陈家第十七代起桩人", "28岁"},
		{"中文数字年龄", "六十七岁，守网人'续脉派'主事", "六十七岁"},
		{"约数来岁", "三十来岁，失踪潜水员何建国的家属", "三十来岁"},
		{"出头约数", "二十出头，周德贵的表侄", "二十出头"},
		{"句中年龄", "陈三斗之父，六十岁上下，名义上二十年前死在三峡", "六十岁上下"},
		{"第三方年龄不误判", "他带着八岁的儿子来报名", ""},
		{"窗口外年龄不取", "这里是一段很长的前置说明文字，用来把窗口占满之后，才提到二十四岁的情况", ""},
		{"零命中", "退役水下工兵，陈家第十七代起桩人", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := matchCharacterBirthYear(c.desc); got != c.want {
				t.Fatalf("matchCharacterBirthYear(%q) = %q, want %q", c.desc, got, c.want)
			}
		})
	}
}

func TestMatchCharacterAliases(t *testing.T) {
	cases := []struct {
		name, desc, want string
	}{
		{"外号水鬼苏", "52岁，疍家出身，外号水鬼苏。水里是神", `["水鬼苏"]`},
		{"人称提取", "守桩人，人称镇水钉。话少", `["镇水钉"]`},
		{"多个别名", "又名阿七，外号七爷", `["阿七","七爷"]`},
		{"零命中", "28岁，退役水下工兵", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := matchCharacterAliases(c.desc); got != c.want {
				t.Fatalf("matchCharacterAliases(%q) = %q, want %q", c.desc, got, c.want)
			}
		})
	}
}

func TestMatchStoryArcCharID(t *testing.T) {
	chars := []arcChar{{ID: 1, Name: "陈三斗"}, {ID: 2, Name: "江知微"}, {ID: 3, Name: "苏阿水"}}
	cases := []struct {
		name, arcName string
		want          int64
	}{
		{"前缀精确命中", "陈三斗：从还债到收桩", 1},
		{"前缀命中另一角色", "江知微：记录者还是选择者", 2},
		{"别名不参与匹配", "水鬼苏的最后一趟水", 0},
		{"子串唯一命中", "陈三斗收桩之路", 1},
		{"多角色命中不写", "陈三斗与江知微的对峙", 0},
		{"零命中不写", "守网人与续脉派", 0},
		{"空名不写", "", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := matchStoryArcCharID(c.arcName, chars); got != c.want {
				t.Fatalf("matchStoryArcCharID(%q) = %d, want %d", c.arcName, got, c.want)
			}
		})
	}
}
