package github

import (
	"context"
	"log"

	"github.com/google/go-github/github"
	"github.com/hashicorp/terraform/helper/schema"
)

func resourceGithubRepositoryCollaborator() *schema.Resource {
	return &schema.Resource{
		Create: resourceGithubRepositoryCollaboratorCreate,
		Read:   resourceGithubRepositoryCollaboratorRead,
		Delete: resourceGithubRepositoryCollaboratorDelete,
		Importer: &schema.ResourceImporter{
			State: schema.ImportStatePassthrough,
		},

		// editing repository collaborators are not supported by github api so forcing new on any changes
		Schema: map[string]*schema.Schema{
			"username": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},
			"repository": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},
			"permission": {
				Type:         schema.TypeString,
				Optional:     true,
				ForceNew:     true,
				Default:      "push",
				ValidateFunc: validateValueFunc([]string{"pull", "push", "admin"}),
			},
		},
	}
}

func resourceGithubRepositoryCollaboratorCreate(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*Organization).client
	username := d.Get("username").(string)
	repoName := d.Get("repository").(string)

	_, err := client.Repositories.AddCollaborator(context.TODO(),
		meta.(*Organization).name,
		repoName,
		username,
		&github.RepositoryAddCollaboratorOptions{
			Permission: d.Get("permission").(string),
		})

	if err != nil {
		return err
	}

	d.SetId(buildTwoPartID(&repoName, &username))

	return resourceGithubRepositoryCollaboratorRead(d, meta)
}

func resourceGithubRepositoryCollaboratorRead(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*Organization).client
	repoName, username, err := parseTwoPartID(d.Id())
	if err != nil {
		return err
	}

	// First, check if the user has been invited but has not yet accepted
	invitation, err := findRepoInvitation(client, meta.(*Organization).name, repoName, username)
	if err != nil {
		return err
	} else if invitation != nil {
		permissionName, err := getInvitationPermission(invitation)
		if err != nil {
			return err
		}

		d.Set("repository", repoName)
		d.Set("username", username)
		d.Set("permission", permissionName)
		return nil
	}

	// Next, check if the user has accepted the invite and is a full collaborator
	opt := &github.ListCollaboratorsOptions{ListOptions: github.ListOptions{PerPage: maxPerPage}}

	for {
		collaborators, resp, err := client.Repositories.ListCollaborators(context.TODO(),
			meta.(*Organization).name, repoName, opt)
		if err != nil {
			return err
		}

		for _, c := range collaborators {
			if *c.Login == username {
				permissionName, err := getRepoPermission(c.Permissions)
				if err != nil {
					return err
				}

				d.Set("repository", repoName)
				d.Set("username", username)
				d.Set("permission", permissionName)
				return nil
			}
		}

		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}

	// The user is neither invited nor a collaborator
	log.Printf("[WARN] GitHub Repository Collaborator (%s) not found, removing from state", d.Id())
	d.SetId("")

	return nil
}

func resourceGithubRepositoryCollaboratorDelete(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*Organization).client
	u := d.Get("username").(string)
	r := d.Get("repository").(string)

	// Delete any pending invitations
	invitation, err := findRepoInvitation(client, meta.(*Organization).name, r, u)
	if err != nil {
		return err
	} else if invitation != nil {
		_, err = client.Repositories.DeleteInvitation(context.TODO(), meta.(*Organization).name, r, *invitation.ID)
		return err
	}

	_, err = client.Repositories.RemoveCollaborator(context.TODO(), meta.(*Organization).name, r, u)
	return err
}

func findRepoInvitation(client *github.Client, owner string, repo string, collaborator string) (*github.RepositoryInvitation, error) {
	opt := &github.ListOptions{PerPage: maxPerPage}
	for {
		invitations, resp, err := client.Repositories.ListInvitations(context.TODO(), owner, repo, opt)
		if err != nil {
			return nil, err
		}

		for _, i := range invitations {
			if *i.Invitee.Login == collaborator {
				return i, nil
			}
		}

		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}
	return nil, nil
}