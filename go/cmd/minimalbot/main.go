package main

import (
	"context"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	qrterminal "github.com/mdp/qrterminal/v3"
	"github.com/oklog/run"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
	"moul.io/srand"
	"moul.io/u"
	"moul.io/zapconfig"

	"berty.tech/berty/v2/go/pkg/bertybot"
	"berty.tech/berty/v2/go/pkg/bertyversion"
	"berty.tech/berty/v2/go/pkg/messengertypes"
)

var (
	username     = u.CurrentUsername("anon")
	node1Addr    = flag.String("addr1", "127.0.0.1:9091", "first remote 'berty daemon' address")
	node2Addr    = flag.String("addr2", "127.0.0.1:9092", "second remote 'berty daemon' address")
	displayName1 = flag.String("name1", username+" (testbot1)", "first bot's display name")
	displayName2 = flag.String("name2", username+" (testbot2)", "second bot's display name")
	debug        = flag.Bool("debug", false, "debug mode")
	logFormat    = flag.String("log-format", "console", strings.Join(zapconfig.AvailablePresets, ", "))
)

func main() {
	flag.Parse()
	rand.Seed(srand.MustSecure())
	if err := Main(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %+v\n", err)
		os.Exit(1)
	}
}

/* type TestBot struct {
	Bot1, Bot2 *bertybot.Bot
	ctx        context.Context
	logger     *zap.Logger
} */

type Bot struct {
	store struct {
		Convs       []*Conversation
		StaffConvPK string
	}
	Bot1, Bot2       *bertybot.Bot
	ctx              context.Context
	client           messengertypes.MessengerServiceClient
	storeIsNew       bool
	storePath        string
	storeConvMap     map[*Conversation]*sync.Mutex
	storeConvMapLock sync.Mutex
	// storeWholeConvLock is a lock where holding it is equivalent to holding all
	// conversations locks.
	storeWholeConvLock sync.RWMutex
	storeMutex         sync.RWMutex
	isReplaying        bool
	logger             *zap.Logger
}

type Conversation struct {
	ConversationPublicKey string
	ContactPublicKey      string
	ContactDisplayName    string
	Step                  uint
	IsOneToOne            bool
}

func Main() error {
	config := zapconfig.Configurator{}
	config.SetPreset(*logFormat)
	logger := config.MustBuild()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// init testbot
	testbot := &Bot{ctx: ctx, logger: logger}
	if err := testbot.InitBot1(); err != nil {
		return fmt.Errorf("init bot 1: %w", err)
	}
	if err := testbot.InitBot2(); err != nil {
		return fmt.Errorf("init bot 2: %w", err)
	}

	// FIXME: bot1 and bot2 should be contact

	// start testbot
	var g run.Group
	g.Add(func() error { return testbot.Bot1.Start(ctx) }, func(error) { cancel() })
	g.Add(func() error { return testbot.Bot2.Start(ctx) }, func(error) { cancel() })
	g.Add(run.SignalHandler(ctx, syscall.SIGKILL))
	return g.Run()
}

func (testbot *Bot) VersionCommand(ctx bertybot.Context) {
	_ = ctx.ReplyString("version: " + bertyversion.Version)
	// FIXME: also returns the version of the remote messenger and protocol
}

// InitBot1 initializes the entrypoint bot
func (testbot *Bot) InitBot1() error {
	logger := testbot.logger.Named("bot1")

	// init bot
	opts := []bertybot.NewOption{}
	opts = append(opts,
		bertybot.WithLogger(logger.Named("lib")),                                  // configure a logger
		bertybot.WithDisplayName(*displayName1),                                   // bot name
		bertybot.WithInsecureMessengerGRPCAddr(*node1Addr),                        // connect to running berty messenger daemon
		bertybot.WithRecipe(bertybot.AutoAcceptIncomingContactRequestRecipe()),    // accept incoming contact requests
		bertybot.WithRecipe(bertybot.WelcomeMessageRecipe("welcome to testbot1")), // send welcome message to new contacts and new conversations
		bertybot.WithRecipe(bertybot.EchoRecipe("you said1: ")),                   // reply to messages with the same message
		// FIXME: with auto-send `/help` suggestion on welcome
		bertybot.WithCommand("version", "show version", testbot.VersionCommand),
	)
	if *debug {
		opts = append(opts, bertybot.WithRecipe(bertybot.DebugEventRecipe(logger.Named("debug")))) // debug events
	}
	bot, err := bertybot.New(opts...)
	if err != nil {
		return fmt.Errorf("bot initialization failed: %w", err)
	}
	// display link and qr code
	logger.Info("retrieve instance Berty ID",
		zap.String("pk", bot.PublicKey()),
		zap.String("link", bot.BertyIDURL()),
	)
	qrterminal.GenerateHalfBlock(bot.BertyIDURL(), qrterminal.L, os.Stdout)
	testbot.Bot1 = bot
	return nil
}

// InitBot2 initializes the companion bot
func (testbot *Bot) InitBot2() error {
	logger := testbot.logger.Named("bot2")

	// init bot
	opts := []bertybot.NewOption{}
	opts = append(opts,
		bertybot.WithLogger(logger.Named("lib")),                                  // configure a logger
		bertybot.WithDisplayName(*displayName2),                                   // bot name
		bertybot.WithInsecureMessengerGRPCAddr(*node2Addr),                        // connect to running berty messenger daemon
		bertybot.WithRecipe(bertybot.AutoAcceptIncomingContactRequestRecipe()),    // accept incoming contact requests
		bertybot.WithRecipe(bertybot.WelcomeMessageRecipe("welcome to testbot2")), // send welcome message to new contacts and new conversations
		bertybot.WithRecipe(bertybot.EchoRecipe("you said2: ")),                   // reply to messages with the same message
	)
	if *debug {
		opts = append(opts, bertybot.WithRecipe(bertybot.DebugEventRecipe(logger.Named("debug")))) // debug events
	}
	bot, err := bertybot.New(opts...)
	if err != nil {
		return fmt.Errorf("bot initialization failed: %w", err)
	}
	// display link and qr code
	logger.Info("retrieve instance Berty ID",
		zap.String("pk", bot.PublicKey()),
		zap.String("link", bot.BertyIDURL()),
	)
	qrterminal.GenerateHalfBlock(bot.BertyIDURL(), qrterminal.L, os.Stdout)
	testbot.Bot2 = bot
	return nil
}

func (bot *Bot) handleUserMessageInteractionUpdated(ctx context.Context, _ *messengertypes.EventStream_Reply, interaction *messengertypes.Interaction, payload proto.Message) error {
	if interaction.IsMine || interaction.Acknowledged {
		return nil
	}

	var conv *Conversation
	bot.storeMutex.RLock()
	for i := range bot.store.Convs {
		if bot.store.Convs[i].ConversationPublicKey == interaction.ConversationPublicKey {
			conv = bot.store.Convs[i]
		}
	}
	bot.storeMutex.RUnlock()
	receivedMessage := payload.(*messengertypes.AppMessage_UserMessage)
	if conv != nil && conv.IsOneToOne {
		unlock := bot.LockConversation(conv)
		success, err := [3]doStepFn{
			doStep0,
			doStep1,
			doStep2,
		}[conv.Step](ctx, conv, bot, receivedMessage, interaction, unlock)
		if err != nil {
			return err
		}
		if success {
			return nil
		}
		// auto-reply to user's messages
		answer := getRandomReply()
		if err := bot.interactUserMessage(ctx, answer, interaction.ConversationPublicKey, defaultReplyOption()); err != nil {
			return fmt.Errorf("interact user message failed: %w", err)
		}
	}
	return nil
}

func doStep2(ctx context.Context, _ *Conversation, bot *Bot, receivedMessage *messengertypes.AppMessage_UserMessage, interaction *messengertypes.Interaction, unlock func()) (bool, error) {
	unlock()
	msg := receivedMessage.GetBody()
	if msg[0] == '/' {
		options := defaultReplyOption()
		switch strings.ToLower(msg[1:]) {
		case "help":
			body := `In this conversation, you can type all these commands :
/demo group
/demo demo
/demo share
/demo contact "Here is the QR code of manfred, just add him!"
/demo version
/lf help "For the help of LFGamings commands"`
			if err := bot.interactUserMessage(ctx, body, interaction.ConversationPublicKey, options); err != nil {
				return false, fmt.Errorf("interact user message failed: %w", err)
			}
		case "demo version":
			var body string
			if bertyversion.VcsRef == "n/a" {
				body = "berty " + bertyversion.Version + "\n" + runtime.Version()
			} else {
				body = "berty " + bertyversion.Version + " https://github.com/berty/berty/commits/" + bertyversion.VcsRef + "\n" + runtime.Version()
			}
			if err := bot.interactUserMessage(ctx, body, interaction.ConversationPublicKey, options); err != nil {
				return false, fmt.Errorf("interact user message failed: %w", err)
			}
		case "lf":
			body := `My first test command :
some next line
and another one
and one more for good luck`
			if err := bot.interactUserMessage(ctx, body, interaction.ConversationPublicKey, options); err != nil {
				return false, fmt.Errorf("interact user message failed: %w", err)
			}
		case "lf payload":
			body := `testing some payloads`
			options := []*messengertypes.ReplyOption{
				{Payload: "yes", Display: "Sure, go for it!"},
				{Payload: "no", Display: "Show me all you can do instead!"},
				{Payload: "test payload", Display: "my testing the payload function!"},
			}
			if err := bot.interactUserMessage(ctx, body, interaction.ConversationPublicKey, options); err != nil {
				return false, fmt.Errorf("interact user message failed: %w", err)
			}
		case "lf wakeup":
			time.Sleep(10 * time.Second)
			body := `grmbl, I am awake,,, now.`
			if err := bot.interactUserMessage(ctx, body, interaction.ConversationPublicKey, options); err != nil {
				return false, fmt.Errorf("interact user message failed: %w", err)
			}
		case "lf wait":
			time.Sleep(10 * time.Second)
			body := `grmbl, I am awake,,, now.`
			if err := bot.interactUserMessage(ctx, body, interaction.ConversationPublicKey, options); err != nil {
				return false, fmt.Errorf("interact user message failed: %w", err)
			}
		case "lf date":
			currentTime := time.Now()
			body := `The current date is: ` + currentTime.Format("02-01-2006") //DD-MM-YYYY
			if err := bot.interactUserMessage(ctx, body, interaction.ConversationPublicKey, options); err != nil {
				return false, fmt.Errorf("interact user message failed: %w", err)
			}
		case "lf time":
			currentTime := time.Now()
			body := `The current time is: ` + currentTime.Format("15:04:05") //Time h-m-s
			if err := bot.interactUserMessage(ctx, body, interaction.ConversationPublicKey, options); err != nil {
				return false, fmt.Errorf("interact user message failed: %w", err)
			}
		case "lf help":
			body := `/lf "my first command I've made"
/lf payload "testing payload commands"
/lf wakeup "'wakeup' the bot"
/lf date "What day is it today?"
/lf time "What is the time"`
			options := []*messengertypes.ReplyOption{
				{Payload: "/lf", Display: "/lf"},
				{Payload: "/lf payload", Display: "/lf payload"},
				{Payload: "/lf help", Display: "/lf help"},
				{Payload: "/lf wakeup", Display: "/lf wakeup"},
				{Payload: "/lf date", Display: "/lf date"},
				{Payload: "/lf time", Display: "/lf time"},
			}
			if err := bot.interactUserMessage(ctx, body, interaction.ConversationPublicKey, options); err != nil {
				return false, fmt.Errorf("interact user message failed: %w", err)
			}
		default:
			body := fmt.Sprintf("Sorry but the command %q is not yet known.", msg)
			if err := bot.interactUserMessage(ctx, body, interaction.ConversationPublicKey, options); err != nil {
				return false, fmt.Errorf("interact user message failed: %w", err)
			}
		}
		return true, nil
	}
	return false, nil
}

func defaultReplyOption() []*messengertypes.ReplyOption {
	return []*messengertypes.ReplyOption{
		{Payload: "/help", Display: "Display betabot commands"},
		{Payload: "/demo version", Display: "What is the demo version?"},
		{Payload: "/lf help", Display: "LFGamings help command"},
	}
}

func getRandomReply() string {
	available := []string{
		"Welcome to the beta!",
		"Hello! Welcome to Berty!",
		"Hey, I hope you're feeling well here!",
		"Hi, I'm here for you at anytime for tests!",
		"Hello dude!",
		"Hello :)",
		"Ow, I like to receive test messages <3",
		"What's up ?",
		"How r u ?",
		"Hello, 1-2, 1-2, check, check?!",
		"Do you copy ?",
		"If you say ping, I'll say pong.",
		"I'm faster than you at sending message :)",
		"One day, bots will rules the world. Or not.",
		"You're so cute.",
		"I like discuss with you, I feel more and more clever.",
		"I'm so happy to chat with you.",
		"I could chat with you all day long.",
		"Yes darling ? Can I help you ?",
		"OK, copy that.",
		"OK, I understand.",
		"Hmmm, Hmmmm. One more time ?",
		"I think you're the most clever human I know.",
		"I missed you babe.",
		"OK, don't send me nudes, I'm a bot dude.",
		"Come on, let's party.",
		"May we have a chat about our love relationship future ?",
		"That's cool. I copy.",
	}
	return available[rand.Intn(len(available))] // nolint:gosec // absolutely no importance in this case
}
