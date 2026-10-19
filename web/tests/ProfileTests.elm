module ProfileTests exposing (suite)

import Domain.Profile as Profile


sample : Profile.Profile
sample =
    { id = 4
    , username = "ada"
    , displayName = "Ada"
    , bio = "Notes"
    , website = "https://ada.example"
    , avatarUrl = "/api/profiles/ada/avatar?v=2"
    , version = 2
    , canEdit = True
    }


caseUsername : Bool
caseUsername =
    Profile.normalizeUsername " Ada_Lane " == Ok "ada_lane"
        && Profile.normalizeUsername "local" /= Ok "local"
        && Profile.normalizeUsername "sign-in" /= Ok "sign-in"
        && Profile.normalizeUsername "a.b" /= Ok "a.b"
        && Profile.normalizeUsername "/ada" /= Ok "ada"


caseWebsite : Bool
caseWebsite =
    Profile.validWebsite "" == Ok ""
        && Profile.validWebsite "https://example.com/path" == Ok "https://example.com/path"
        && Profile.validWebsite "javascript:alert(1)" /= Ok "javascript:alert(1)"
        && Profile.validWebsite "http://user:pass@example.com" /= Ok "http://user:pass@example.com"


caseConflictKeepsDraft : Bool
caseConflictKeepsDraft =
    let
        draft =
            Profile.draftFrom sample

        edited =
            { draft | displayName = "Countess", username = "ada_lane" }

        remote =
            { sample | version = 5, displayName = "Someone else" }

        kept =
            Profile.applyRemoteVersion remote edited
    in
    kept.displayName == "Countess"
        && kept.username == "ada_lane"
        && kept.expectedVersion == 5
        && Profile.isDirty sample edited


caseCover : Bool
caseCover =
    Profile.safeCover "/media/previews/1.jpg" == Just "/media/previews/1.jpg"
        && Profile.safeCover "https://example.com/a.jpg" == Nothing
        && Profile.safeCover "/media/../secret.jpg" == Nothing
        && Profile.initials "Ada Lovelace" == "AL"


suite : List ( String, Bool )
suite =
    [ ( "profile username rules", caseUsername )
    , ( "profile website rules", caseWebsite )
    , ( "stale profile edit keeps the draft", caseConflictKeepsDraft )
    , ( "profile covers stay on media paths", caseCover )
    ]
