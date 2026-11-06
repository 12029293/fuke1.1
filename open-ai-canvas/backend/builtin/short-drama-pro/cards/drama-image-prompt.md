---
name: drama-image-prompt
description: "图片生成提示词写法：角色板、场景板、道具板的结构，静态锚点与视觉锁定，可复制提示词的语法。当要写人物形象图提示词、场景图提示词、道具图提示词，或生成的图与设定不一致时调用。"
tags: ["图片提示词", "角色板", "场景板", "一致性锚点", "提示词语法"]
metadata:
  source: "影策短剧创作台"
---

# 图片提示词：一致性靠锚点，不靠运气

## 三种板子

| 板子 | 用途 | 一条提示词包含 |
|---|---|---|
| 角色板 | 锁定人物外观，供分镜生图引用 | 身份锚点 + 本集造型 + 姿态 + 背景 + 画质 |
| 场景板 | 锁定空间地理，供首帧生图引用 | 空间结构 + 固定锚点 + 光线 + 视角 |
| 道具板 | 锁定关键物件形制 | 形制 + 材质 + 细节标记 + 状态 |

## 提示词语言

**默认英文。** 主流图像模型对英文的解析更稳定。中文提示词只在模型明确支持中文时使用。

提示词正文**可直接复制**，不含占位符、不含流程说明、不含「请生成」这类指令语。

## 角色板结构

```text
[年龄与身份], [发型发色], [面部特征], [体态], wearing [服装细节],
[姿态与表情], standing against [背景], [光线], [画质词], no text, no logo
```

示例：

```text
a 99-year-old Chinese man with a long white beard, thin and slightly stooped,
deep-set eyes with heavy wrinkles, wearing a dark grey-blue patched Chinese cotton
jacket with cloth buttons and a faded indigo collar, holding a dark brown wooden
walking cane in his right hand, standing upright facing the camera with a calm
stubborn expression, plain neutral grey background, soft even studio lighting,
full body character sheet, photorealistic, no text, no logo
```

要点：
- **身份锚点**（面部特征、永久标记）必须出现，且用词固定，不要每次换说法
- **造型**写清材质、颜色、细节（补丁、纽扣、磨损），越具体越稳定
- **姿态**固定为「站立、面向镜头、全身」，这样角色板可比对
- **背景**用中性背景，避免场景元素干扰
- **负面**用 `no text, no logo` 等尾部否定，不写「不要模糊」这类无效否定

## 场景板结构

```text
[空间类型] in [时代/地域], [空间结构], [固定锚点], [材质与磨损],
[光线方向与色温], [视角与焦段], [氛围], no people, no text, no logo
```

示例：

```text
the dim L-shaped stairwell of an old Chinese residential building,
mottled beige plaster walls with peeling paint, a green metal door frame
with faded red couplets on both sides, a bare energy-saving bulb casting
warm tungsten light from the left, concrete steps with worn edges,
shot at eye level with a 35mm lens, quiet and slightly melancholic atmosphere,
no people, no text, no logo
```

要点：
- **固定锚点**是空间识别的关键（门框颜色、对联、灯具），必须每张都出现
- **光线**写方向 + 色温，这是氛围的主要来源
- **视角与焦段**写清，避免每次生成都是不同角度
- 场景板通常 `no people`，除非要展示人物在空间中的尺度

## 道具板结构

```text
[物件类型], [形制与尺寸], [材质], [颜色], [关键细节], [状态],
[摆放方式], [光线], [画质], no text, no logo
```

## 一致性锚点的写法

一致性不靠模型「记住」，靠**每次提示词里出现同一组固定短语**。

在视觉设定里先写 `LOCK-xxx`，然后在所有相关提示词里**逐字复制**这个短语：

```md
LOCK-父亲：a 99-year-old Chinese man with a long white beard, thin and slightly stooped
LOCK-女儿：a 75-year-old Chinese woman with silver-grey ear-length hair and thin round metal glasses
LOCK-楼道：mottled beige plaster walls, a green metal door frame with faded red couplets
```

**规则**：LOCK 短语一旦定下，全篇不许改词序、不许换同义词。改一个字，模型就会当成另一个人。

## 从视频反推时的写法

1. **先读视觉设定**的 LOCK 短语，直接引用
2. **每个角色一条角色板提示词**，不要合并
3. **每个地点一条场景板**，同一地点的不同视图单列
4. **只写看得见的**。原片没出现的细节（比如「她戴着手链」）不要加
5. **拿不准的用概括**。看不清材质就写 `dark fabric`，不要编 `wool` 或 `leather`

## 常见问题

| 现象 | 原因 | 修法 |
|---|---|---|
| 每次生成的人物脸都不一样 | LOCK 短语不一致 | 逐字复制 LOCK，不换词 |
| 场景每张都不同 | 固定锚点没写全 | 补门框/灯具/材质等识别特征 |
| 画面出现文字 | 没写负面 | 尾部加 `no text, no logo` |
| 人物服装细节丢失 | 描述太笼统 | 写材质、颜色、磨损、配饰 |
| 生成了多余的人 | 场景板没写 `no people` | 场景板尾部加 `no people` |
| 提示词里有占位符 | 用了模板 | 提示词必须是可直接复制的成品 |

## 交付格式

```md
## IMG-FU-SHEET · 父亲角色板

- 控制：身份、体态、本集造型
- 关联：视觉设定「人物 · 父亲」

### 可复制提示词

> a 99-year-old Chinese man with a long white beard, thin and slightly stooped, ...
```

`IMG-xxx` 是稳定 ID，分镜与视频提示词按它引用。
