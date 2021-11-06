package main

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/pechorka/whattobuy/moex"
	"github.com/pechorka/whattobuy/store"
	"github.com/pkg/errors"

	tb "gopkg.in/tucnak/telebot.v2"
)

type Bot struct {
	telebot *tb.Bot
	store   *store.Store
	mapi    *moex.API
}

type Opts struct {
	Token      string
	Timeout    time.Duration
	WebHookURL string
	Store      *store.Store
	MoexAPI    *moex.API
	TLSKey     string
	TLSCert    string
}

func (opts *Opts) getPoller() tb.Poller {
	if opts.WebHookURL != "" {
		return &tb.Webhook{
			Listen: opts.WebHookURL,
			TLS: &tb.WebhookTLS{
				Key:  opts.TLSKey,
				Cert: opts.TLSCert,
			},
		}
	}

	return &tb.LongPoller{
		Timeout: opts.Timeout,
	}
}

func NewBot(opts *Opts) (*Bot, error) {
	telebot, err := tb.NewBot(tb.Settings{
		Token:  opts.Token,
		Poller: opts.getPoller(),
	})

	if err != nil {
		return nil, err
	}
	b := &Bot{
		telebot: telebot,
		store:   opts.Store,
		mapi:    opts.MoexAPI,
	}
	b.handle()
	return b, nil
}

func (b *Bot) Start() {
	b.telebot.Start()
}

func (b *Bot) Stop() {
	b.telebot.Stop()
}

func (b *Bot) handle() {
	b.telebot.Handle("/start", b.onStart)
	b.telebot.Handle(tb.OnText, b.onText)
	b.telebot.Handle("/view", b.onView)
	b.telebot.Handle("/buy", b.onBuy)
	b.telebot.Handle("/finish", b.onFinish)
	b.telebot.Handle("/restart", b.onRestart)
}

func (b *Bot) onStart(m *tb.Message) {
	if b.isUserFinished(m) {
		b.reply(m, "У вас уже заполнен портфель. Для ввода портфеля заново воспользуйтесь командой /restart")
		return
	}
	b.reply(m, "Начните вводить желаемую структуру вашего портфеля сообщениями вида: 'тикер процент'.  Например, FXMM 30 или RU000A0JS1W0 10. В одном сообщении может быть несколько позиций - каждая на новой строчке. Когда закончите ввод, введите /finish. Проценты должны суммироваться в 100. Если где-то ошиблись, то введите эту позицию заново - процент заменится. Для удаления позиции обнулите её. Для глобальных изменнкний есть команда /restart :)")
}

func (b *Bot) onText(m *tb.Message) {
	if b.isUserFinished(m) {
		b.reply(m, "У вас уже заполнен портфель. Для ввода портфеля заново воспользуйтесь командой /restart")
		return
	}

	readInput := func(s string) (string, float64, error) {
		input := strings.Split(s, " ")
		if len(input) != 2 {

			return "", 0, errors.New("Некорректный формат: ожидается формат 'тикер процент'")
		}

		percent, err := strconv.ParseFloat(input[1], 64)
		if err != nil {
			return "", 0, errors.Wrap(err, "Укажите количество корректно, сейчас так: ")
		}

		return input[0], percent, nil
	}

	var (
		userInput  = make(map[string]float64)
		sumPercent float64
		notFound   []string
	)
	for _, s := range strings.Split(m.Text, "\n") {
		secid, percent, err := readInput(s)
		if err != nil {
			b.onInvalidInput(m, err)
			return
		}
		secid = strings.ToUpper(secid)
		_, err = b.mapi.Get(context.TODO(), secid)
		if err == moex.ErrNotFound {
			_, err = b.mapi.Get(context.TODO(), secid+"-RM")
			if err == nil {
				secid += "-RM"
			}
		}
		if err != nil {
			log.Printf("[ERROR] while fetching data from moex: %v\n", err)
			notFound = append(notFound, secid)
			continue
		}
		userInput[secid] = percent
		sumPercent += percent
	}

	partfolio, err := b.store.GetPartfolio(m.Sender.ID)
	if err != nil {
		b.onError(m, errors.Wrap(err, "error while retriving portfolio"))
		return
	}

	var sp float64
	for secid, p := range partfolio {
		if _, ok := userInput[secid]; ok { // we replace current value, no need to count it
			continue
		}
		sp += p
	}

	if sp+sumPercent > 100 {
		b.onInvalidInput(m, errors.Errorf("Нельзя добавить такой процент, будет больше 100. Доступно для ввода %.2f, а сейчас есть %.2f", 100-sp, sp))
		return
	}

	if err = b.store.AddToPartfolio(m.Sender.ID, userInput); err != nil {
		b.onError(m, errors.Wrap(err, "error while upadating portfolio"))
		return
	}
	if len(notFound) > 0 {
		found := make([]string, 0, len(userInput))
		for secid := range userInput {
			found = append(found, secid)
		}
		var reply string
		if len(found) > 0 {
			reply = "Были добавлены бумаги:\n" + strings.Join(found, "\n") + "\n"
		}
		reply += "Часть бумаг была не найдена:\n" + strings.Join(notFound, "\n")
		b.reply(m, reply)
		return
	}
	b.reply(m, "Успешно изменено")

	if sp+sumPercent == 100 {
		var reply strings.Builder
		reply.WriteString("Сумма долей достигла 100%. Хотите завершить ввод портфеля - нажмите /finish. Портфель на данный момент выглядит так:\n")
		for secid, percent := range partfolio {
			reply.WriteString(fmt.Sprintf("%s - %.2f%%\n", noRM(secid), percent))
		}
		for secid, percent := range userInput {
			reply.WriteString(fmt.Sprintf("%s - %.2f%%", noRM(secid), percent))
		}
		b.reply(m, reply.String())
	}
}

func (b *Bot) onFinish(m *tb.Message) {
	partfolio, err := b.store.GetPartfolio(m.Sender.ID)
	if err != nil {
		b.onError(m, errors.Wrap(err, "error while retriving portfolio"))
		return
	}
	var sp float64
	for _, p := range partfolio {
		sp += p
	}
	if sp < 100 {
		b.onInvalidInput(m, errors.Errorf("В вашем портфель доли складываются не в 100%%, а в %.2f%%", sp))
		return
	}
	if err := b.store.Finish(m.Sender.ID); err != nil {
		b.onError(m, errors.Wrap(err, "error while finishing user portfolio"))
		return
	}
	b.reply(m, `Ваш портфель успешно сохранен.
Для просмотра его содержимого введите команду /view.
Для того чтобы узнать, что купить на заданную сумму, введите '/buy сумма'`)
}

func (b *Bot) onRestart(m *tb.Message) {
	partfolio, err := b.store.GetPartfolio(m.Sender.ID)
	if err != nil {
		b.onError(m, errors.Wrap(err, "error while retriving portfolio"))
		return
	}
	if err := b.store.ClearData(m.Sender.ID); err != nil {
		b.onError(m, errors.Wrap(err, "error while deleting portfolio"))
		return
	}
	var reply strings.Builder
	reply.WriteString("Ваш портфель удалён. На случай, если вы сделали это случайно, скопируйте это сообщение:\n")
	for secid, percent := range partfolio {
		reply.WriteString(fmt.Sprintf("%s %.2f\n", noRM(secid), percent))
	}
	b.reply(m, reply.String())
}

func noRM(secid string) string {
	return strings.TrimSuffix(secid, "-RM")
}

func (b *Bot) onView(m *tb.Message) {
	partfolio, err := b.store.GetPartfolio(m.Sender.ID)
	if err != nil {
		b.onError(m, errors.Wrap(err, "error while retriving portfolio"))
		return
	}
	infos, err := b.loadSecurityPrices(context.TODO(), m, partfolio)
	if err != nil {
		b.onError(m, errors.Wrap(err, "error while retriving prices"))
		return
	}

	if len(partfolio) == 0 {
		b.reply(m, "Портфель сейчас пуст. Добавляйте сообщения вида 'тикер процент'")
		return
	}

	var reply strings.Builder
	reply.WriteString("содержимое вашего портфеля\n")
	for secid, percent := range partfolio {
		reply.WriteString(fmt.Sprintf("(%.2f%%) %s - %q\n", percent, noRM(secid), infos[secid].ShortName))
	}
	b.reply(m, reply.String())
}

func (b *Bot) onBuy(m *tb.Message) {
	if !b.isUserFinished(m) {
		b.reply(m, "У вас еще не заполнен портфель или вы не ввели команду /finish")
		return
	}
	capital, err := strconv.ParseFloat(m.Payload, 32)
	if err != nil {
		b.onInvalidInput(m, errors.Wrapf(err, "Сумма на покупку не число, а %s\n", m.Payload))
		return
	}
	partfolio, err := b.store.GetPartfolio(m.Sender.ID)
	if err != nil {
		b.onError(m, errors.Wrap(err, "error while retriving portfolio"))
		return
	}
	infos, err := b.loadSecurityPrices(context.TODO(), m, partfolio)
	if err != nil {
		b.onError(m, errors.Wrap(err, "error while retriving prices"))
		return
	}
	var (
		reply      strings.Builder
		totalSpend float64
	)
	for secid, percent := range partfolio {
		info := infos[secid]
		sum := capital * percent / 100
		lots := sum / info.Price
		if lots < info.LotSize {
			if lots == 0 {
				reply.WriteString(fmt.Sprintf("%s - %.2f%% 💩 суммы недостаточно, чтобы купить 1 ценную бумагу. Она стоит %.2f, что меньше %.2f. \n", secid, percent, info.Price, sum))
				continue
			} else {
				reply.WriteString(fmt.Sprintf("%s - %.2f%% 💩 суммы недостаточно, чтобы купить 1 лот (можно купить %.0f ценных бумаг, а в одном лоте %.0f ценных бумаг)\n", secid, percent, lots, info.LotSize))
				continue
			}
		}
		lots /= info.LotSize
		lots = float64(int(lots))
		spendMoney := lots * info.Price * info.LotSize
		reply.WriteString(fmt.Sprintf("%s - %.0f лотов (на %.2f у.е.)\n", secid, lots, spendMoney))
		totalSpend += spendMoney
	}
	reply.WriteString(fmt.Sprintf("\n🥳Итого на покупку уйдет %.2f рублей", totalSpend))
	b.reply(m, reply.String())
}

func (b *Bot) loadSecurityPrices(ctx context.Context, m *tb.Message, partfolio store.Partfolio) (map[string]moex.StockInfo, error) {
	secids := make([]string, 0, len(partfolio))
	for secid := range partfolio {
		secids = append(secids, secid)
	}
	return b.mapi.GetMultiple(ctx, secids...)
}

func (b *Bot) onError(m *tb.Message, err error) {
	log.Printf("[ERROR] %v", err)
	b.reply(m, "Ошибка на сервере: "+err.Error())
}

func (b *Bot) onInvalidInput(m *tb.Message, err error) {
	b.reply(m, "Неверный ввод: "+err.Error())
}

func (b *Bot) reply(m *tb.Message, msg string) {
	_, err := b.telebot.Reply(m, msg)
	if err != nil {
		log.Printf("[ERROR] while replying: %v", err)
	}
}

func (b *Bot) isUserFinished(m *tb.Message) bool {
	finished, err := b.store.IsUserFinished(m.Sender.ID)
	if err != nil {
		b.onError(m, errors.Wrap(err, "error while checking user state"))
		return false
	}
	if !finished {
		return false
	}
	return true
}
