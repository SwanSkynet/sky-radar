package main

import (
	"log"

	"automation/activities"
	"automation/workflow"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

const TaskQueue = "claude-pipeline"

func main() {
	c, err := client.Dial(client.Options{
		HostPort: "localhost:7233",
	})
	if err != nil {
		log.Fatalln("unable to create Temporal client:", err)
	}
	defer c.Close()

	w := worker.New(c, TaskQueue, worker.Options{})

	w.RegisterWorkflow(workflow.PipelineWorkflow)
	w.RegisterActivity(activities.GetNextTask)
	w.RegisterActivity(activities.MarkComplete)
	w.RegisterActivity(activities.RunClaude)
	w.RegisterActivity(activities.AddressFeedback)
	w.RegisterActivity(activities.CreatePR)
	w.RegisterActivity(activities.FetchReviewStatus)
	w.RegisterActivity(activities.WaitForChecks)
	w.RegisterActivity(activities.MergePR)

	log.Println("Worker started. Listening on task queue:", TaskQueue)

	if err := w.Run(worker.InterruptCh()); err != nil {
		log.Fatalln("unable to start worker:", err)
	}
}
