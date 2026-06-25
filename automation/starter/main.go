package main

import (
	"context"
	"log"

	"automation/workflow"

	"go.temporal.io/sdk/client"
)

func main() {
	c, err := client.Dial(client.Options{HostPort: "localhost:7233"})
	if err != nil {
		log.Fatalln("unable to create client:", err)
	}
	defer c.Close()

	options := client.StartWorkflowOptions{
		ID:        "claude-pipeline-run",
		TaskQueue: "claude-pipeline",
	}

	we, err := c.ExecuteWorkflow(context.Background(), options, workflow.PipelineWorkflow)
	if err != nil {
		log.Fatalln("unable to start workflow:", err)
	}

	log.Println("Started pipeline. WorkflowID:", we.GetID(), "RunID:", we.GetRunID())
}
