package kafcli

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"golang.org/x/net/html/charset"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"
)

type Book struct {
	Filename       string    // 目录
	Bookname       string    // 书名
	Match          string    // 正则
	VolumeMatch    string    // 卷匹配规则
	Author         string    // 作者
	Max            uint      // 标题最大字数
	Indent         uint      // 段落缩进字段
	Align          string    // 标题对齐方式
	UnknowTitle    string    // 未知章节名称
	Cover          string    // 封面图片
	CoverOrlyColor string    // 生成封面图片的颜色
	CoverOrlyIdx   int       // 生成封面图片的动物
	Font           string    // 嵌入字体
	Bottom         string    // 段阿落间距
	LineHeight     string    // 行高
	Tips           bool      // 是否添加教程文本
	Lang           string    // 设置语言
	Out            string    // 输出文件名
	Format         string    // 书籍格式
	SectionList    []Section // 章节
	Decoder        *encoding.Decoder
	PageStylesFile string
	Reg            *regexp.Regexp
	VolumeReg      *regexp.Regexp
	version        string
}

type Section struct {
	Title    string
	Content  string
	Sections []Section
}

type Converter interface {
	Build(book Book) error
}

const (
	htmlPStart         = `<p class="content">`
	htmlPEnd           = "</p>"
	htmlTitleStart     = `<h3 class="title">`
	mobiTtmlTitleStart = `<h3 style="text-align:%s;">`
	htmlTitleEnd       = "</h3>"
	VolumeMatch        = "^第[0-9一二三四五六七八九十零〇百千两 ]+[卷部]"
	DefaultMatchTips   = "^第[0-9一二三四五六七八九十零〇百千两 ]+[章回节集卷部]|^[Ss]ection.{1,20}$|^[Cc]hapter.{1,20}$|^[Pp]age.{1,20}$|^\\d{1,4}$|^引子$|^楔子$|^章节目录|^章节|^序章"
	cssContent         = `
.title {text-align:%s}
.content {
  margin-bottom: %s;
  margin-top: 0;
  text-indent: %dem;
  %s
}
`
	Tutorial = ``
)

func NewBookSimple(filename string) (*Book, error) {
	book := Book{
		Filename:       filename,
		Bookname:       "",
		Match:          DefaultMatchTips,
		VolumeMatch:    VolumeMatch,
		Author:         "PENWYP",
		UnknowTitle:    "章节正文",
		Max:            35,
		Indent:         2,
		Align:          GetEnv("KAF_CLI_ALIGN", "center"),
		Cover:          "cover.png",
		Bottom:         "1em",
		Tips:           true,
		Lang:           GetEnv("KAF_CLI_LANG", "zh"),
		Out:            "",
		Format:         GetEnv("KAF_CLI_FORMAT", "all"),
		SectionList:    nil,
		Decoder:        nil,
		PageStylesFile: "",
		Reg:            nil,
	}
	if os.Getenv("KAF_CLI_LANG") != "" {
		book.Lang = os.Getenv("KAF_CLI_LANG")
	}
	return &book, nil
}

func NewBookArgs() *Book {
	var book Book
	flag.StringVar(&book.Filename, "filename", "", "txt 文件名")
	flag.StringVar(&book.Author, "author", "PENWYP", "作者")
	flag.StringVar(&book.Bookname, "bookname", "", "书名: 默认为txt文件名")
	flag.UintVar(&book.Max, "max", 35, "标题最大字数")
	flag.StringVar(&book.Match, "match", "", "匹配标题的正则表达式, 不写可以自动识别, 如果没生成章节就参考教程。例: -match 第.{1,8}章 表示第和章字之间可以有1-8个任意文字")
	flag.StringVar(&book.VolumeMatch, "volume-match", VolumeMatch, "卷匹配规则")
	flag.StringVar(&book.UnknowTitle, "unknow-title", "章节正文", "未知章节默认名称")
	flag.UintVar(&book.Indent, "indent", 2, "段落缩进字数")
	flag.StringVar(&book.Align, "align", GetEnv("KAF_CLI_ALIGN", "center"), "标题对齐方式: left、center、righ。环境变量KAF_CLI_ALIGN可修改默认值")
	flag.StringVar(&book.Cover, "cover", "cover.png", "封面图片可为: 本地图片, 和orly。 设置为orly时生成orly风格的封面, 需要连接网络。")
	flag.StringVar(&book.CoverOrlyColor, "cover-orly-color", "", "orly封面的主题色, 可以为1-16和hex格式的颜色代码, 不填时随机")
	flag.IntVar(&book.CoverOrlyIdx, "cover-orly-idx", -1, "orly封面的动物, 可以为0-41, 不填时随机, 具体图案可以查看: https://orly.nanmu.me")
	flag.StringVar(&book.Bottom, "bottom", "1em", "段落间距(单位可以为em、px)")
	flag.StringVar(&book.LineHeight, "line-height", "", "行高(用于设置行间距, 默认为1.5rem)")
	flag.StringVar(&book.Font, "font", "", "嵌入字体, 之后epub的正文都将使用该字体")
	flag.StringVar(&book.Format, "format", GetEnv("KAF_CLI_FORMAT", "all"), "书籍格式: all、epub、mobi、azw3。环境变量KAF_CLI_FORMAT可修改默认值")
	flag.StringVar(&book.Lang, "lang", GetEnv("KAF_CLI_LANG", "zh"), "设置语言: en,de,fr,it,es,zh,ja,pt,ru,nl。环境变量KAF_CLI_LANG可修改默认值")
	flag.StringVar(&book.Out, "out", "", "输出文件名，不需要包含格式后缀")
	flag.BoolVar(&book.Tips, "tips", false, "添加本软件教程")
	flag.Parse()
	return &book
}

func (book *Book) Check(version string) error {
	book.version = version
	if !strings.HasSuffix(book.Filename, ".txt") {
		return errors.New("不是txt文件")
	}
	if book.Filename == "" {
		fmt.Println("错误: 文件名不能为空")
		fmt.Println("软件版本: \t", version)
		fmt.Println("简洁模式: \t把文件拖放到kaf-cli上")
		fmt.Println("命令行简单模式: kaf-cli ebook.txt")
		fmt.Println("\n以下为kaf-cli的全部参数")
		flag.PrintDefaults()
		if runtime.GOOS == "windows" {
			time.Sleep(time.Second * 10)
		}
		os.Exit(0)
	}
	// 通过文件名解析书名
	reg, _ := regexp.Compile(`《(.*)》.*作者：(.*).txt`)
	if reg.MatchString(book.Filename) {
		group := reg.FindAllStringSubmatch(book.Filename, -1)
		if len(group) == 1 && len(group[0]) >= 3 {
			if book.Bookname == "" {
				book.Bookname = group[0][1]
			}
			if book.Author == "" || book.Author == "PENWYP" {
				book.Author = group[0][2]
			}
		}
	}
	if book.Bookname == "" {
		book.Bookname = strings.Split(filepath.Base(book.Filename), ".")[0]
	}
	if book.Out == "" {
		book.Out = book.Bookname
	}
	book.Lang = parseLang(book.Lang)
	switch book.Cover {
	case "none":
		book.Cover = ""
	case "gen", "orly":
		cover, err := GenCover(book.Bookname, book.Author, book.CoverOrlyColor, book.CoverOrlyIdx)
		if err != nil {
			panic(err)
		}
		book.Cover = cover
	default:
		if exists, _ := isExists(book.Cover); !exists {
			book.Cover = ""
		}
	}

	// 编译正则表达式
	if book.Match == "" {
		book.Match = DefaultMatchTips
	}
	reg, err := regexp.Compile(book.Match)
	if err != nil {
		return fmt.Errorf("生成匹配规则出错: %s\n%s\n", book.Match, err.Error())
	}
	book.Reg = reg
	reg2, err := regexp.Compile(book.VolumeMatch)
	if err != nil {
		return fmt.Errorf("生成匹配规则出错: %s\n%s\n", book.VolumeMatch, err.Error())
	}
	book.VolumeReg = reg2
	return nil
}

func (book *Book) readBuffer(filename string) *bufio.Reader {
	f, err := os.Open(filename)
	if err != nil {
		fmt.Println("读取文件出错: ", err.Error())
		os.Exit(1)
	}
	temBuf := bufio.NewReader(f)
	bs, _ := temBuf.Peek(1024)
	encodig, encodename, _ := charset.DetermineEncoding(bs, "text/plain")
	if encodename != "utf-8" {
		f.Seek(0, 0)
		bs, err := ioutil.ReadAll(f)
		if err != nil {
			fmt.Println("读取文件出错: ", err.Error())
			os.Exit(1)
		}
		var buf bytes.Buffer
		book.Decoder = encodig.NewDecoder()
		if encodename == "windows-1252" {
			book.Decoder = simplifiedchinese.GB18030.NewDecoder()
		}
		bs, _, _ = transform.Bytes(book.Decoder, bs)
		buf.Write(bs)
		return bufio.NewReader(&buf)
	} else {
		f.Seek(0, 0)
		buf := bufio.NewReader(f)
		return buf
	}
}

func (book Book) ToString() {
	fmt.Println("转换信息:")
	fmt.Println("软件版本:", book.version)
	fmt.Println("文件名:\t", book.Filename)
	fmt.Println("书籍书名:", book.Bookname)
	fmt.Println("书籍作者:", book.Author)
	if book.Cover != "" {
		fmt.Println("书籍封面:", book.Cover)
	}
	fmt.Println("书籍语言:", book.Lang)
	if book.Match == DefaultMatchTips {
		fmt.Println("匹配条件:", "自动匹配")
	} else {
		fmt.Println("匹配条件:", book.Match)
	}
	fmt.Println("卷匹配条件:", book.VolumeMatch)
	fmt.Println("转换格式:", book.Format)
	fmt.Println()
}

func (book *Book) Parse() error {
	fmt.Println("正在读取txt文件...")
	start := time.Now()
	buf := book.readBuffer(book.Filename)

	var contentList []Section
	var title string
	var content bytes.Buffer

	for {
		line, err := buf.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				if line != "" {
					if line = strings.TrimSpace(line); line != "" {
						addPart(&content, line)
					}
				}
				contentList = append(contentList, Section{
					Title:   title,
					Content: content.String(),
				})
				content.Reset()
				break
			}
			return fmt.Errorf("读取文件出错: %w", err)
		}
		line = strings.TrimSpace(line)
		line = strings.ReplaceAll(line, "<", "&lt;")
		line = strings.ReplaceAll(line, ">", "&gt;")
		for range []int{1, 2, 3, 4, 5} {
			line = strings.ReplaceAll(line, "===", "")
		}

		// 空行直接跳过
		if len(line) == 0 {
			continue
		}
		// 处理标题
		if utf8.RuneCountInString(line) <= int(book.Max) &&
			(book.Reg.MatchString(line) || book.VolumeReg.MatchString(line)) {
			if title == "" {
				title = book.UnknowTitle
			}
			if content.Len() > 0 || title != book.UnknowTitle {
				contentList = append(contentList, Section{
					Title:   title,
					Content: content.String(),
				})
			}
			title = line
			content.Reset()
			continue
		}
		addPart(&content, line)
	}
	// 没识别到章节又没识别到 EOF 时，把所有的内容写到最后一章
	if content.Len() != 0 {
		if title == "" {
			title = "章节正文"
		}
		contentList = append(contentList, Section{
			Title:   title,
			Content: content.String(),
		})
	}
	var sectionList []Section
	var volumeSection *Section
	for _, section := range contentList {
		if book.VolumeReg.MatchString(section.Title) {
			if volumeSection != nil {
				sectionList = append(sectionList, *volumeSection)
				volumeSection = nil
			}
			temp := section
			volumeSection = &temp
		} else {
			if volumeSection == nil {
				sectionList = append(sectionList, section)
			} else {
				volumeSection.Sections = append(volumeSection.Sections, section)
			}
		}
	}
	// 如果有最后一卷,添加到章节列表
	if volumeSection != nil {
		sectionList = append(sectionList, *volumeSection)
		volumeSection = nil
	}
	end := time.Now().Sub(start)
	fmt.Println("读取文件耗时:", end)
	fmt.Println("匹配章节:", sectionCount(sectionList))
	// 添加提示
	if book.Tips {
		section := Section{
			Title:   "制作说明",
			Content: Tutorial,
		}
		sectionList = append([]Section{section}, sectionList...)
		sectionList = append(sectionList, section)
	}
	book.SectionList = sectionList
	return nil
}

func sectionCount(sections []Section) int {
	var count int
	for _, section := range sections {
		count += 1 + len(section.Sections)
	}
	return count
}

func (book *Book) Convert() {
	start := time.Now()
	// 解析文本
	fmt.Println()

	// 判断要生成的格式
	var isEpub, isMobi, isAzw3 bool
	switch book.Format {
	case "epub":
		isEpub = true
	case "mobi":
		isEpub = true
		isMobi = true
	case "azw3":
		isAzw3 = true
	default:
		isEpub = true
		isMobi = true
		isAzw3 = true
	}

	hasKinldegen := lookKindlegen()
	if book.Format == "mobi" && hasKinldegen == "" {
		isEpub = false
	}

	var convert Converter
	// 生成epub
	if isEpub {
		convert = EpubConverter{}
		convert.Build(*book)
		fmt.Println()
	}
	// 生成azw3格式
	if isAzw3 {
		convert = Azw3Converter{}
		// 生成kindle格式
		convert.Build(*book)
	}
	// 生成mobi格式
	if isMobi {
		if hasKinldegen == "" {
			convert = MobiConverter{}
			convert.Build(*book)
		} else {
			converToMobi(fmt.Sprintf("%s.epub", book.Out), book.Lang)
		}
	}
	end := time.Now().Sub(start)
	fmt.Println("\n转换完成! 总耗时:", end)
}

func addPart(buff *bytes.Buffer, content string) {
	if strings.HasSuffix(content, "==") ||
		strings.HasSuffix(content, "**") ||
		strings.HasSuffix(content, "--") ||
		strings.HasSuffix(content, "//") {
		buff.WriteString(content)
		return
	}
	buff.WriteString(htmlPStart)
	buff.WriteString(content)
	buff.WriteString(htmlPEnd)
}
