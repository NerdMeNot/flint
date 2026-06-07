package engine

import (
	"testing"

	"github.com/NerdMeNot/flint/pkg/pipeline"
	"github.com/stretchr/testify/assert"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
)

func jobWithStep() *batchv1.Job {
	return &batchv1.Job{Spec: batchv1.JobSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
		Containers: []corev1.Container{{Name: "step"}, {Name: "svc-db"}},
	}}}}
}

func TestApplyStepResources_SetsStepContainer(t *testing.T) {
	job := jobWithStep()
	applyStepResources(job, &pipeline.StepResources{
		CPU: "2", Memory: "4Gi",
		Limits: &pipeline.StepResourceLimits{CPU: "3", Memory: "6Gi"},
	})
	c := job.Spec.Template.Spec.Containers[0]
	assert.Equal(t, "2", c.Resources.Requests.Cpu().String())
	assert.Equal(t, "4Gi", c.Resources.Requests.Memory().String())
	assert.Equal(t, "3", c.Resources.Limits.Cpu().String())
	assert.Equal(t, "6Gi", c.Resources.Limits.Memory().String())
}

func TestApplyStepResources_NilIsNoop(t *testing.T) {
	job := jobWithStep()
	applyStepResources(job, nil)
	assert.True(t, job.Spec.Template.Spec.Containers[0].Resources.Requests.Cpu().IsZero())
}

func TestApplyStepResources_InvalidQuantitySkipped(t *testing.T) {
	job := jobWithStep()
	// A bad quantity must not panic or set a value.
	applyStepResources(job, &pipeline.StepResources{CPU: "not-a-qty", Memory: "4Gi"})
	c := job.Spec.Template.Spec.Containers[0]
	assert.True(t, c.Resources.Requests.Cpu().IsZero())
	assert.Equal(t, "4Gi", c.Resources.Requests.Memory().String())
}
