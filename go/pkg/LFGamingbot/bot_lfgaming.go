package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"

	qrterminal "github.com/mdp/qrterminal/v3"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"moul.io/zapconfig"

	"berty.tech/berty/v2/go/pkg/bertybot"
	"berty.tech/berty/v2/go/pkg/messengertypes"
)

var (
	nodeAddr      = flag.String("addr", "127.0.0.1:9091", "remote 'berty daemon' address")
	logFormat     = flag.String("log-format", "console", strings.Join(zapconfig.AvailablePresets, ", "))
)

func main() {
	if err := bot_lfgaming(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %+v\n", err)
		os.Exit(1)
	}
}

func bot_lfgaming() error {
	// create context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// init logger
	var botLogger *zap.Logger
	{
		config := zapconfig.Configurator{}
		config.SetPreset(*logFormat)
		logger, err := config.Build()
		if err != nil {
			return fmt.Errorf("build zap logger failed: %w", err)
		}
		botLogger = logger
	}

	// init messenger gRPC client
	var botClient messengertypes.MessengerServiceClient
	{
		cc, err := grpc.DialContext(ctx, *nodeAddr, grpc.WithInsecure())
		if err != nil {
			return fmt.Errorf("unable to connect with remote berty messenger node: %w", err)
		}
		botClient = messengertypes.NewMessengerServiceClient(cc)
	}

	// create bot
	var bot *bertybot.Bot
	{
		var err error
		bot, err = bertybot.New(
			bertybot.WithLogger(botLogger),
			bertybot.WithMessengerClient(botClient),
			bertybot.WithRecipe(bertybot.DebugEventRecipe(botLogger)),
			bertybot.WithRecipe(bertybot.AutoAcceptIncomingContactRequestRecipe()),
		)
		if err != nil {
			return fmt.Errorf("bertybot new failed: %w", err)
		}
	}

	// start bot in goroutine
	go func() {
		err := bot.Start(ctx)
		if err != nil {
			fmt.Errorf("bertybot start failed: %w", err)
		}
	}()

	// get BertyIDURL and show it
	{
		botLogger.Info("retrieve instance Berty ID",
			zap.String("pk", bot.PublicKey()),
			zap.String("link", bot.BertyIDURL()),
		)
		qrterminal.GenerateHalfBlock(bot.BertyIDURL(), qrterminal.L, os.Stdout)
	}

	// event loop
	var wg sync.WaitGroup
	{
		wg.Add(1)
		go func() {
			defer wg.Done()
			// TODO: handle events
		}()
	}

	waitForCtrlC(ctx, cancel)
	wg.Wait()
	return nil
}

func waitForCtrlC(ctx context.Context, cancel context.CancelFunc) {
	signalChannel := make(chan os.Signal, 1)
	signal.Notify(signalChannel, os.Interrupt)

	select {
	case <-signalChannel:
		cancel()
	case <-ctx.Done():
	}
}
