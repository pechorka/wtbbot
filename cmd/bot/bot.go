package main

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/pechorka/whattobuy/moex"
	"github.com/pechorka/whattobuy/store"
	"github.com/pkg/errors"
	"gopkg.in/tucnak/telebot.v2"
	tb "gopkg.in/tucnak/telebot.v2"
)

type Bot struct {
	telebot *tb.Bot
	store   *store.Store
	mapi    *moex.API
}

type Opts struct {
	Token   string
	Timeout time.Duration
	Store   *store.Store
	MoexAPI *moex.API
}

func NewBot(opts Opts) (*Bot, error) {
	telebot, err := tb.NewBot(tb.Settings{
		Token: opts.Token,
		Poller: &tb.LongPoller{
			Timeout: opts.Timeout,
		},
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
	b.telebot.Handle("/view", b.view)
	b.telebot.Handle("/buy", b.buy)
}

func (b *Bot) onStart(m *tb.Message) {
	if b.isUserFinished(m) {
		return
	}
	b.reply(m, "Начните вводить содержимое вашего партфолио сообщениями вида: тикер процент. Когда закончите ввод, введите /finish. Проценты должны суммироваться в 100")
}

func (b *Bot) onText(m *tb.Message) {
	if b.isUserFinished(m) {
		return
	}

	input := strings.Split(m.Text, " ")
	if len(input) != 2 {
		b.onInvalidInput(m, errors.New("ожидается формат тикер процент"))
		return
	}

	percent, err := strconv.ParseFloat(input[1], 64)
	if err != nil {
		b.onInvalidInput(m, errors.Wrap(err, "процент не число"))
		return
	}

	partfolio, err := b.store.GetPartfolio(m.Sender.ID)
	if err != nil {
		b.onError(m, errors.Wrap(err, "while retriving partfolio"))
		return
	}

	var sp float64
	for secid, p := range partfolio {
		if secid == input[0] {
			continue
		}
		sp += p
	}

	if sp+percent > 100 {
		b.onInvalidInput(m, errors.Errorf("Нельзя добавить такой процент, будет больше 100. Доступно для ввода %f", 100-sp))
		return
	}

	if err = b.store.AddToPartfolio(m.Sender.ID, input[0], percent); err != nil {
		b.onError(m, errors.Wrap(err, "error while upadating partfolio"))
		return
	}
	b.reply(m, "Успешно добавлено")
}

func (b *Bot) view(m *tb.Message) {
	partfolio, err := b.store.GetPartfolio(m.Sender.ID)
	if err != nil {
		b.onError(m, errors.Wrap(err, "error while retriving partfolio"))
		return
	}
	infos, err := b.mapi.GetAllSecuritiesPrices(moex.EngineStock, moex.MarketShares)
	if err != nil {
		b.onError(m, errors.Wrap(err, "error while retriving prices"))
		return
	}
	var reply strings.Builder
	for secid := range partfolio {
		reply.WriteString(fmt.Sprintf("%s - %q\n", secid, infos[secid].ShortName))
	}
	b.reply(m, reply.String())
}

func (b *Bot) buy(m *tb.Message) {
	if b.isUserFinished(m) {
		return
	}
	montlySum, err := strconv.ParseFloat(m.Payload, 32)
	if err != nil {
		b.onInvalidInput(m, errors.Wrap(err, "сумма на покупку не число"))
		return
	}
	partfolio, err := b.store.GetPartfolio(m.Sender.ID)
	if err != nil {
		b.onError(m, errors.Wrap(err, "error while retriving partfolio"))
		return
	}
	info, err := b.mapi.GetAllSecuritiesPrices(moex.EngineStock, moex.MarketShares)
	if err != nil {
		b.onError(m, errors.Wrap(err, "error while retriving prices"))
		return
	}
	var reply strings.Builder
	for secid, percent := range partfolio {
		sum := montlySum * percent / 100
		lots := sum / info[secid].Price
		reply.WriteString(fmt.Sprintf("%s - %f\n", secid, lots))
	}
	b.reply(m, reply.String())
}

func (b *Bot) onError(m *telebot.Message, err error) {
	log.Printf("[ERROR] %v", err)
	b.reply(m, "Ошибка на сервере: "+err.Error())
}

func (b *Bot) onInvalidInput(m *telebot.Message, err error) {
	b.reply(m, "Неверный ввод: "+err.Error())
}

func (b *Bot) reply(m *telebot.Message, msg string) {
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
	if finished {
		b.reply(m, "У вас уже заполнено партфолио. Для ввода партфолио заново воспользуйтесь командой /restart")
		return false
	}
	return true
}
