package admiral

import (
	"context"
	"fmt"
	v12 "k8s.io/api/batch/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/client-go/informers/batch/v1"
	"sync"
	"time"

	"github.com/istio-ecosystem/admiral/admiral/pkg/client/loader"
	"github.com/istio-ecosystem/admiral/admiral/pkg/controller/common"
	log "github.com/sirupsen/logrus"
	"k8s.io/client-go/rest"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

//Job controller discovers jobs as mesh clients (its assumed that k8s Job doesn't have any ingress communication)

type JobController struct {
	K8sClient  kubernetes.Interface
	JobHandler ClientDiscoveryHandler
	informer   cache.SharedIndexInformer
	Cache      *jobCache
}

type JobEntry struct {
	Identity string
	Jobs     map[string]*common.K8sObject
}

type jobCache struct {
	//map of dependencies key=identity value array of onboarded identities
	cache map[string]*JobEntry
	mutex *sync.Mutex
}

func NewJobCache() *jobCache {
	return &jobCache{
		cache: make(map[string]*JobEntry),
		mutex: &sync.Mutex{},
	}
}

func getK8sObjectFromJob(job *v12.Job) *common.K8sObject {
	return &common.K8sObject{
		Name:        job.Name,
		Namespace:   job.Namespace,
		Annotations: job.Spec.Template.Annotations,
		Labels:      job.Spec.Template.Labels,
		Status:      common.NotProcessed,
		Type:        common.Job,
	}
}

func (p *jobCache) Put(job *common.K8sObject) *common.K8sObject {
	defer p.mutex.Unlock()
	p.mutex.Lock()
	identity := common.GetGlobalIdentifier(job.Annotations, job.Labels)
	existingJobs := p.cache[identity]
	if existingJobs == nil {
		existingJobs = &JobEntry{
			Identity: identity,
			Jobs:     map[string]*common.K8sObject{job.Namespace: job},
		}
		p.cache[identity] = existingJobs
		return job
	} else {
		jobInCache := existingJobs.Jobs[job.Namespace]
		if jobInCache == nil {
			existingJobs.Jobs[job.Namespace] = job
			p.cache[identity] = existingJobs
			return job
		}
	}
	return job
}

func (p *jobCache) Get(key string, namespace string) *common.K8sObject {
	defer p.mutex.Unlock()
	p.mutex.Lock()

	jce, ok := p.cache[key]
	if ok {
		j, ok := jce.Jobs[namespace]
		if ok {
			return j
		}
	}
	return nil
}

func (p *jobCache) GetJobProcessStatus(job *v12.Job) (string, error) {
	defer p.mutex.Unlock()
	p.mutex.Lock()
	jobObj := getK8sObjectFromJob(job)
	identity := common.GetGlobalIdentifier(jobObj.Annotations, jobObj.Labels)

	jce, ok := p.cache[identity]
	if ok {
		jobFromNamespace, ok := jce.Jobs[job.Namespace]
		if ok {
			return jobFromNamespace.Status, nil
		}
	}

	return common.NotProcessed, nil
}

func (p *jobCache) UpdateJobProcessStatus(job *v12.Job, status string) error {
	defer p.mutex.Unlock()
	p.mutex.Lock()
	jobObj := getK8sObjectFromJob(job)
	identity := common.GetGlobalIdentifier(jobObj.Annotations, jobObj.Labels)

	jce, ok := p.cache[identity]
	if ok {
		jobFromNamespace, ok := jce.Jobs[job.Namespace]
		if ok {
			jobFromNamespace.Status = status
			p.cache[jce.Identity] = jce
			return nil
		} else {
			newJob := getK8sObjectFromJob(job)
			newJob.Status = status
			jce.Jobs[job.Namespace] = newJob
			p.cache[jce.Identity] = jce
			return nil
		}
	}

	return fmt.Errorf(LogCacheFormat, "UpdateStatus", "Job",
		job.Name, job.Namespace, "", "nothing to update, job not found in cache")
}

func (p *JobController) DoesGenerationMatch(ctxLogger *log.Entry, obj interface{}, oldObj interface{}) (bool, error) {
	if !common.DoGenerationCheck() {
		ctxLogger.Debugf(ControllerLogFormat, "DoesGenerationMatch", "",
			fmt.Sprintf("generation check is disabled"))
		return false, nil
	}
	jobNew, ok := obj.(*v12.Job)
	if !ok {
		return false, fmt.Errorf("type assertion failed, %v is not of type *Job", obj)
	}
	jobOld, ok := oldObj.(*v12.Job)
	if !ok {
		return false, fmt.Errorf("type assertion failed, %v is not of type *Job", oldObj)
	}
	if jobNew.Generation == jobOld.Generation {
		ctxLogger.Infof(ControllerLogFormat, "DoesGenerationMatch", "",
			fmt.Sprintf("old and new generation matched for job %s", jobNew.Name))
		return true, nil
	}
	return false, nil
}

func (p *JobController) IsOnlyReplicaCountChanged(*log.Entry, interface{}, interface{}) (bool, error) {
	return false, nil
}

func NewJobController(stopCh <-chan struct{}, handler ClientDiscoveryHandler, config *rest.Config, resyncPeriod time.Duration, clientLoader loader.ClientLoader) (*JobController, error) {

	jobController := JobController{}
	jobController.JobHandler = handler

	var err error

	jobController.K8sClient, err = clientLoader.LoadKubeClientFromConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create dependency controller k8s client: %v", err)
	}

	jobController.informer = v1.NewJobInformer(
		jobController.K8sClient,
		meta_v1.NamespaceAll,
		resyncPeriod,
		cache.Indexers{},
	)

	jobController.Cache = NewJobCache()

	NewController("job-ctrl", config.Host, stopCh, &jobController, jobController.informer)

	return &jobController, nil
}

func (d *JobController) Added(ctx context.Context, obj interface{}) error {
	return addUpdateJob(d, ctx, obj)
}

func (d *JobController) Updated(ctx context.Context, obj interface{}, oldObj interface{}) error {
	//Not Required, this is a no-op as as Add event already handles registering this as a mesh client
	return nil
}

func addUpdateJob(j *JobController, ctx context.Context, obj interface{}) error {
	job, ok := obj.(*v12.Job)
	if !ok {
		return fmt.Errorf("failed to covert informer object to Job")
	}
	if !common.ShouldIgnore(job.Spec.Template.Annotations, job.Spec.Template.Labels) {
		k8sObj := getK8sObjectFromJob(job)
		newK8sObj := j.Cache.Put(k8sObj)
		newK8sObj.Status = common.ProcessingInProgress
		return j.JobHandler.Added(ctx, newK8sObj)
	}
	return nil
}

func (p *JobController) Deleted(ctx context.Context, obj interface{}) error {
	//Not Required (to be handled via asset off boarding)
	return nil
}

func (d *JobController) GetProcessItemStatus(obj interface{}) (string, error) {
	job, ok := obj.(*v12.Job)
	if !ok {
		return common.NotProcessed, fmt.Errorf("type assertion failed, %v is not of type *common.K8sObject", obj)
	}
	return d.Cache.GetJobProcessStatus(job)
}

func (d *JobController) UpdateProcessItemStatus(obj interface{}, status string) error {
	job, ok := obj.(*v12.Job)
	if !ok {
		return fmt.Errorf("type assertion failed, %v is not of type *Job", obj)
	}
	return d.Cache.UpdateJobProcessStatus(job, status)
}

func (d *JobController) LogValueOfAdmiralIoIgnore(obj interface{}) {
	job, ok := obj.(*v12.Job)
	if !ok {
		return
	}
	jobObj := getK8sObjectFromJob(job)
	if jobObj.Annotations[common.AdmiralIgnoreAnnotation] == "true" {
		log.Infof("op=%s type=%v name=%v namespace=%s cluster=%s message=%s", "admiralIoIgnoreAnnotationCheck", common.MonoVertex,
			job.Name, job.Namespace, "", "Value=true")
	}
}

func (j *JobController) Get(ctx context.Context, isRetry bool, obj interface{}) (interface{}, error) {
	job, ok := obj.(*v12.Job)
	if ok && isRetry {
		jobObj := getK8sObjectFromJob(job)
		identity := common.GetGlobalIdentifier(jobObj.Annotations, jobObj.Labels)
		return j.Cache.Get(identity, job.Namespace), nil
	}
	if ok && j.K8sClient != nil {
		return j.K8sClient.BatchV1().Jobs(job.Namespace).Get(ctx, job.Name, meta_v1.GetOptions{})
	}
	return nil, fmt.Errorf("kubernetes client is not initialized, txId=%s", ctx.Value("txId"))
}
