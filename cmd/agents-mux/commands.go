package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	mux "github.com/prxg22/agents-mux"
)

// --- session ---

var sessionCmd = &cobra.Command{
	Use:   "session",
	Short: "Manage sessions",
}

var sessionListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all sessions",
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr, err := newManager()
		if err != nil {
			return err
		}
		defer mgr.Close()

		sessions, err := mgr.ListSessions()
		if err != nil {
			return err
		}
		if sessions == nil {
			sessions = []mux.Session{}
		}

		format, _ := cmd.Flags().GetString("format")
		if format == "table" {
			return printSessionTable(sessions)
		}
		return printJSON(sessions)
	},
}

var sessionCreateCmd = &cobra.Command{
	Use:   "create [id]",
	Short: "Create a new session",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr, err := newManager()
		if err != nil {
			return err
		}
		defer mgr.Close()

		id := ""
		if len(args) > 0 {
			id = args[0]
		}
		if err := mgr.CreateSession(id); err != nil {
			return err
		}
		if id == "" {
			fmt.Println("Session created")
		} else {
			fmt.Printf("Session %s created\n", id)
		}
		return nil
	},
}

var sessionDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a session",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr, err := newManager()
		if err != nil {
			return err
		}
		defer mgr.Close()

		if err := mgr.DeleteSession(args[0]); err != nil {
			return err
		}
		fmt.Printf("Session %s deleted\n", args[0])
		return nil
	},
}

var sessionMessagesCmd = &cobra.Command{
	Use:   "messages <id>",
	Short: "Show messages for a session",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr, err := newManager()
		if err != nil {
			return err
		}
		defer mgr.Close()

		msgs, err := mgr.GetMessages(args[0])
		if err != nil {
			return err
		}
		return printJSON(msgs)
	},
}

var sessionStopCmd = &cobra.Command{
	Use:   "stop <id>",
	Short: "Stop an active session",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr, err := newManager()
		if err != nil {
			return err
		}
		defer mgr.Close()

		if err := mgr.Stop(args[0]); err != nil {
			return err
		}
		fmt.Printf("Session %s stopped\n", args[0])
		return nil
	},
}

// --- prompt ---

var promptAgent string
var promptModel string
var promptSessionID string

var promptCmd = &cobra.Command{
	Use:   "prompt [text]",
	Short: "Send a prompt to an agent",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		text := strings.Join(args, " ")
		if yesMode {
			return runPromptNonInteractive(text)
		}
		return runPromptInteractive(text)
	},
}

func runPromptNonInteractive(text string) error {
	mgr, err := newManager()
	if err != nil {
		return err
	}
	defer mgr.Close()

	result, err := mgr.Send(mux.SendRequest{
		Prompt:    text,
		SessionID: promptSessionID,
		Agent:     promptAgent,
		Model:     promptModel,
	})
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "Session: %s\n", result.SessionID)

	for evt := range result.Events {
		switch evt.Type {
		case mux.ChanText:
			fmt.Print(evt.Text)
		case mux.ChanAction:
			fmt.Fprintf(os.Stderr, "[action] %s\n", evt.JSON)
		case mux.ChanAskUser:
			fmt.Fprintf(os.Stderr, "[ask_user] %s\n", evt.JSON)
		}
	}
	fmt.Println()
	return nil
}

// --- queue ---

var queueCmd = &cobra.Command{
	Use:   "queue",
	Short: "Manage the follow-up queue",
}

var queueSessionID string

var queueListCmd = &cobra.Command{
	Use:   "list",
	Short: "List queue items for a session",
	RunE: func(cmd *cobra.Command, args []string) error {
		if queueSessionID == "" {
			return fmt.Errorf("--session is required")
		}
		mgr, err := newManager()
		if err != nil {
			return err
		}
		defer mgr.Close()

		items, err := mgr.ListQueue(queueSessionID)
		if err != nil {
			return err
		}
		return printJSON(items)
	},
}

var queueAddCmd = &cobra.Command{
	Use:   "add [text]",
	Short: "Add a queue item",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if queueSessionID == "" {
			return fmt.Errorf("--session is required")
		}
		mgr, err := newManager()
		if err != nil {
			return err
		}
		defer mgr.Close()

		text := strings.Join(args, " ")
		item, err := mgr.AddToQueue(queueSessionID, text, "", "", "", "", nil, "text", "")
		if err != nil {
			return err
		}
		return printJSON(item)
	},
}

var queueDeleteCmd = &cobra.Command{
	Use:   "delete <item-id>",
	Short: "Delete a queue item",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if queueSessionID == "" {
			return fmt.Errorf("--session is required")
		}
		mgr, err := newManager()
		if err != nil {
			return err
		}
		defer mgr.Close()

		if err := mgr.DeleteQueueItem(queueSessionID, args[0]); err != nil {
			return err
		}
		fmt.Printf("Queue item %s deleted\n", args[0])
		return nil
	},
}

var queueClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Clear all pending queue items for a session",
	RunE: func(cmd *cobra.Command, args []string) error {
		if queueSessionID == "" {
			return fmt.Errorf("--session is required")
		}
		mgr, err := newManager()
		if err != nil {
			return err
		}
		defer mgr.Close()

		mgr.ClearQueue(queueSessionID)
		fmt.Println("Queue cleared")
		return nil
	},
}

// --- agents ---

var agentsCmd = &cobra.Command{
	Use:   "agents",
	Short: "Manage agent registry",
}

var agentsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available agents",
	RunE: func(cmd *cobra.Command, args []string) error {
		registry := mux.NewRegistry()
		registry.Discover()
		agents := registry.ListAgents()

		format, _ := cmd.Flags().GetString("format")
		if format == "table" {
			return printAgentTable(agents)
		}
		return printJSON(agents)
	},
}

// --- init subcommands ---

func init() {
	sessionListCmd.Flags().String("format", "json", "output format: json or table")
	sessionCmd.AddCommand(sessionListCmd)
	sessionCmd.AddCommand(sessionCreateCmd)
	sessionCmd.AddCommand(sessionDeleteCmd)
	sessionCmd.AddCommand(sessionMessagesCmd)
	sessionCmd.AddCommand(sessionStopCmd)

	promptCmd.Flags().StringVarP(&promptAgent, "agent", "a", "", "agent to use (claude, codex, opencode, cursor)")
	promptCmd.Flags().StringVarP(&promptModel, "model", "m", "", "model override")
	promptCmd.Flags().StringVarP(&promptSessionID, "session", "s", "", "session ID (creates new if empty)")

	queueListCmd.Flags().StringVarP(&queueSessionID, "session", "s", "", "session ID")
	queueAddCmd.Flags().StringVarP(&queueSessionID, "session", "s", "", "session ID")
	queueDeleteCmd.Flags().StringVarP(&queueSessionID, "session", "s", "", "session ID")
	queueClearCmd.Flags().StringVarP(&queueSessionID, "session", "s", "", "session ID")
	queueCmd.AddCommand(queueListCmd)
	queueCmd.AddCommand(queueAddCmd)
	queueCmd.AddCommand(queueDeleteCmd)
	queueCmd.AddCommand(queueClearCmd)

	agentsListCmd.Flags().String("format", "json", "output format: json or table")
	agentsCmd.AddCommand(agentsListCmd)
}

// --- helpers ---

func printJSON(v interface{}) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func printSessionTable(sessions []mux.Session) error {
	if len(sessions) == 0 {
		fmt.Println("No sessions")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tSTATUS\tTITLE\tAGENT\tLAST ACTIVE")
	for _, s := range sessions {
		t := time.Unix(s.LastActiveAt, 0).Format("2006-01-02 15:04")
		agent := s.LastAgent
		if agent == "" {
			agent = "-"
		}
		title := s.Title
		if title == "" {
			title = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", s.ID, s.Status, title, agent, t)
	}
	return w.Flush()
}

func printAgentTable(agents []mux.AgentInfo) error {
	if len(agents) == 0 {
		fmt.Println("No agents")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tAVAILABLE\tDEFAULT MODEL")
	for _, a := range agents {
		avail := "no"
		if a.Available {
			avail = "yes"
		}
		model := a.DefaultModel
		if model == "" {
			model = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", a.ID, a.Name, avail, model)
	}
	return w.Flush()
}
