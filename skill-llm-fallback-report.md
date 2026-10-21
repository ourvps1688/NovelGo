# LLM 兜底分类命中率报告

- 生成时间: 2026-10-20 14:28:38
- Provider/Model: `deepseek` / `deepseek-flash`
- 数据集规模: 29 条（正样例 23，负面 6）

## 汇总

| 指标 | 命中 | 总数 | 命中率 |
|---|---|---|---|
| 整体 | 28 | 29 | 96.6% |
| 确定性 cue（未走 LLM） | 1 | 1 | 100.0% |
| **LLM 兜底子集** | 27 | 28 | **96.4%** |
| 负面误触发 | 0 | 6 | 0.0% |

## 明细

| 消息 | 期望 | 路由到 | 来源 | 结果 |
|---|---|---|---|---|
| 我想搭一套自洽的力量强弱逻辑和进阶路线，前后别矛盾 | worldbuilding-design | worldbuilding-design | llm | ✅ |
| 两个国家打仗，我想让双方的兵力对比和科技水平都更有说服力 | worldbuilding-design | worldbuilding-design | llm | ✅ |
| 这个新对手我想让他每次登场都透着危险感，但做事动机要合理可信 | character-design | character-design | llm | ✅ |
| 主角失忆了，我想后面慢慢揭开他不为人知的过去 | character-design | story-blueprint | llm | ❌ |
| 整本书从第一页到结局的大结构该怎么搭才不散，我怕写到后面收不回来 | story-blueprint | story-blueprint | llm | ✅ |
| 头几章怎么写才能让人忍不住一直读下去，别一上来就劝退 | opening-chapters | opening-chapters | llm | ✅ |
| 新书刚发，前两章没人看，是不是开场太温了 | opening-chapters | opening-chapters | cue | ✅ |
| 我写的这段打斗总觉得平，缺乏让人紧张起来的张力 | scene-beats | scene-beats | llm | ✅ |
| 这两章读着有点发闷，推进不够利落，读者容易划走 | pacing-control | pacing-control | llm | ✅ |
| 高潮那段我想让它一口气冲到底，别在中途泄气 | pacing-control | pacing-control | llm | ✅ |
| 每一节的末尾我想留个让人惦记下文的东西，勾着往下翻 | plot-hook | plot-hook | llm | ✅ |
| 两个人说话总觉得太直白，话里没有藏着别的意思 | dialogue-subtext | dialogue-subtext | llm | ✅ |
| 长辈训话那段，我想让每句话都带刺又不直接说破 | dialogue-subtext | dialogue-subtext | llm | ✅ |
| 描写总像在旁边报幕，读者没法跟着主角一同经历那些时刻 | reader-immersion | reader-immersion | llm | ✅ |
| 这段读起来很假，像是按公式凑出来的，没有活人该有的语感 | deai-flavor | deai-flavor | llm | ✅ |
| 请把这段调得像是真人随手写的，别那么工整 | deai-flavor | deai-flavor | llm | ✅ |
| 初稿写完了，我想整体过一遍把不顺的地方理平 | revision-pass | revision-pass | llm | ✅ |
| 我想知道这类题材在平台上受不受欢迎，有没有商业前景 | signability-audit | signability-audit | llm | ✅ |
| 脑子空白，一点主意都冒不出来 | brainstorm-composer | brainstorm-composer | llm | ✅ |
| 写完这章你帮我看看水平怎么样，挑挑毛病 | review | review | llm | ✅ |
| 按之前定好的走向，把后面的内容接着往下写 | next | next | llm | ✅ |
| 我想给这部书想一句让人忍不住点开去看的话 | book-pitch | book-pitch | llm | ✅ |
| 动笔前我想先把整本书的线索和坑都拢一遍，免得后面穿帮 | collect | collect | llm | ✅ |
| 你好 | (不命中) |  | none | ✅ |
| 今天心情不错 | (不命中) |  | none | ✅ |
| 帮我查下明天上海的天气预报 | (不命中) |  | none | ✅ |
| 谢谢你的帮忙 | (不命中) |  | none | ✅ |
| 这软件怎么导出文件 | (不命中) |  | none | ✅ |
| 我想写点东西 | (不命中) |  | none | ✅ |
